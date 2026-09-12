package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	"github.com/masahikokawai/training-go/bff-gin/backend/internal/model"
	"github.com/masahikokawai/training-go/bff-gin/backend/internal/repository"
)

// userRepository はUserServiceが必要とする最小のリポジトリ操作
// *repository.Userがこれらのメソッドを実装しているため本番の配線
// (cmd/server/main.go)は変更不要だが、テストではインメモリのfakeに
// 差し替えられるようにする
//
// 【2回目のテスト監査で変更】
// UpdateRole→UpdateRoleGuarded に変更(最後の管理者ガードと更新を同一トランザクションにするため、TOCTOUレース対策)
//
// Get/CountManagement はガード判定が repository 層へ移ったことでservice層から呼ばなくなったため削除した
// (使われなくなったメソッドをインターフェースに残さない、という既存の「最小のリポジトリ操作」方針をそのまま踏襲)
type userRepository interface {
	UpsertByKeycloakSub(ctx context.Context, keycloakSub, email, name string, role model.Role) (*model.User, error)
	List(ctx context.Context) ([]model.User, error)
	UpdateRoleGuarded(ctx context.Context, id uint64, role model.Role) error
	Create(ctx context.Context, name, email, passwordDigest string, role model.Role, passwordExpiresAt time.Time) (*model.User, error)
	Delete(ctx context.Context, id uint64) error
}

// passwordExpiryOnAdminCreate はadmin画面から作成したローカル認証ユーザーの password_expires_at
// パスワード再設定画面を用意していない(CONTRACT.mdセクション16.2)
// スコープのため、seedユーザー(2099年固定)と同じ考え方で「作成時刻+100年」を
// 運用上の無期限として扱う
const passwordExpiryOnAdminCreate = 100 * 365 * 24 * time.Hour

// passkeyChecker はUserService.Listがhas_passkeyを計算するために使う最小の依存
// (CONTRACT.mdセクション22.4・22.7)
// *repository.WebauthnCredential がこれを実装する
type passkeyChecker interface {
	UserIDsWithPasskey(ctx context.Context) (map[uint64]bool, error)
}

// UserService はJITプロビジョニング・role管理のユースケース
type UserService struct {
	repo     userRepository
	passkeys passkeyChecker
}

func NewUserService(repo userRepository) *UserService {
	return &UserService{repo: repo}
}

// WithPasskeyChecker は has_passkey 計算用の依存を後付けで設定する
//
// 既存の呼び出し元(NewUserService(repo)のみ)を一切変更せずに済むよう、
// 必須引数ではなくオプションの setter にしている
//
// 設定しなければ List は全ユーザー HasPasskey=false を返す
func (s *UserService) WithPasskeyChecker(checker passkeyChecker) *UserService {
	s.passkeys = checker
	return s
}

type UserDTO struct {
	ID         uint64
	Name       string
	Email      string
	Role       string
	HasPasskey bool
}

func toUserDTO(u model.User) UserDTO {
	return UserDTO{ID: u.ID, Name: u.Name, Email: u.Email, Role: u.Role.String()}
}

// Provision はBFFの `/api/auth/callback` から呼ばれるJITプロビジョニング
// CONTRACT.md セクション10: KeycloakのID Tokenクレーム(sub/name/email/realm_access.roles)を
// 元に、アプリ側の users テーブルへ upsert する
// Rails版の `resources :users`(サインアップ)を置き換える処理にあたる
func (s *UserService) Provision(ctx context.Context, keycloakSub, name, email string, keycloakRoles []string) (UserDTO, error) {
	role := model.RoleGeneral
	for _, r := range keycloakRoles {
		if r == "management" {
			role = model.RoleManagement
			break
		}
	}
	user, err := s.repo.UpsertByKeycloakSub(ctx, keycloakSub, email, name, role)
	if err != nil {
		return UserDTO{}, fmt.Errorf("JITプロビジョニング: %w", err)
	}
	return toUserDTO(*user), nil
}

// List はAdmin::Users一覧相当
func (s *UserService) List(ctx context.Context) ([]UserDTO, error) {
	users, err := s.repo.List(ctx)
	if err != nil {
		return nil, err
	}
	var passkeySet map[uint64]bool
	if s.passkeys != nil {
		passkeySet, err = s.passkeys.UserIDsWithPasskey(ctx)
		if err != nil {
			return nil, err
		}
	}
	dtos := make([]UserDTO, 0, len(users))
	for _, u := range users {
		dto := toUserDTO(u)
		dto.HasPasskey = passkeySet[u.ID]
		dtos = append(dtos, dto)
	}
	return dtos, nil
}

// UpdateRole はAdmin::Users#update相当
// Rails版のバリデーション「最後の管理者をgeneralに変更できない」を再現する
//
// 【2回目のテスト監査で変更】
// 以前はここで「Get→CountManagement(参照)
// → repo.UpdateRole(更新)」を別々の非トランザクションなクエリとして呼んでおり、
// 2つの並行リクエストが同時に降格しようとするとガードをすり抜けられる
// TOCTOUレースがあった(実際にgoroutineで再現・確認済み)
//
// ガードと更新を同一トランザクション内で行う`repo.UpdateRoleGuarded`に一本化し、
// repository 層のセンチネル(repository.ErrLastManagerUser)をここで service 層の ErrLastManagerUser へ翻訳する
// (isDuplicateEmailError と同じ「repository 層の事情を service 層が知って翻訳する」既存パターン)
func (s *UserService) UpdateRole(ctx context.Context, id uint64, role model.Role) error {
	if err := s.repo.UpdateRoleGuarded(ctx, id, role); err != nil {
		if errors.Is(err, repository.ErrLastManagerUser) {
			return ErrLastManagerUser
		}
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("%w", ErrNotFound)
		}
		return err
	}
	return nil
}

// Create はadmin画面からのローカル認証ユーザー新規作成(CONTRACT.mdセクション17.2)
// Keycloak経由のユーザーはJITプロビジョニング(Provision)で自動作成されるため対象外
func (s *UserService) Create(ctx context.Context, name, email, password string, role model.Role) (UserDTO, error) {
	// 【テスト監査で発見・修正】emailの前後空白を除去せずに保存していたため、
	// " user@example.com"(先頭空白付き)と"user@example.com"が別レコードとして
	// 作成できてしまっていた(MySQLのUNIQUE制約はutf8mb4_general_ci collationの
	// PAD SPACE挙動により末尾空白の差異は偶然吸収するが、先頭空白は吸収しない。
	// 大文字小文字の違いはcollationが元々case-insensitiveなため問題ない)
	// admin/go・admin/rails側でも入力欄のtrimは保証されていないため、ここで正規化する
	email = strings.TrimSpace(email)
	if name == "" || email == "" || password == "" {
		return UserDTO{}, fmt.Errorf("%w: name/email/passwordは必須です", ErrValidation)
	}
	digest, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return UserDTO{}, fmt.Errorf("パスワードのハッシュ化に失敗しました: %w", err)
	}
	expiresAt := time.Now().Add(passwordExpiryOnAdminCreate)
	user, err := s.repo.Create(ctx, name, email, string(digest), role, expiresAt)
	if err != nil {
		if isDuplicateEmailError(err) {
			return UserDTO{}, ErrEmailTaken
		}
		return UserDTO{}, fmt.Errorf("ユーザー作成: %w", err)
	}
	return toUserDTO(*user), nil
}

// isDuplicateEmailError はMySQLのUNIQUE制約違反(users.email、migrations/000001)を検出する
// GORMは特定のDBエラーコードを直接返すため、文字列マッチで判別する
// (training-go/gin/internal/repository/user.goと同じ判定方法)
func isDuplicateEmailError(err error) bool {
	return strings.Contains(err.Error(), "Duplicate entry") && strings.Contains(err.Error(), "email")
}

// Delete はAdmin::Users#destroy相当(CONTRACT.mdセクション17.3)
// 「最後の管理者は変更・削除できない」ガードは、UpdateRoleと同じ理由で
// repository.User.Delete内のトランザクションに移した(TOCTOUレース対策、UpdateRoleのコメント参照)
func (s *UserService) Delete(ctx context.Context, id uint64) error {
	if err := s.repo.Delete(ctx, id); err != nil {
		if errors.Is(err, repository.ErrLastManagerUser) {
			return ErrLastManagerUser
		}
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("%w", ErrNotFound)
		}
		return fmt.Errorf("ユーザー削除: %w", err)
	}
	return nil
}
