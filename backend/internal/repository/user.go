package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/masahikokawai/training-go/bff-gin/backend/internal/model"
)

// isDuplicateUserRaceError はJITプロビジョニングの並行レース(UpsertByKeycloakSub)で
// users.email または user_keycloaks.keycloak_sub のUNIQUE制約違反が起きたことを検出する
// service/user.goのisDuplicateEmailErrorと同じ判定方法(文字列マッチ)
func isDuplicateUserRaceError(err error) bool {
	msg := err.Error()
	if !strings.Contains(msg, "Duplicate entry") {
		return false
	}
	return strings.Contains(msg, "index_users_on_email") || strings.Contains(msg, "idx_user_keycloaks_keycloak_sub")
}

// User はusersテーブルへのアクセスを担当する
type User struct {
	db *gorm.DB
}

func NewUser(db *gorm.DB) *User {
	return &User{db: db}
}

// UpsertByKeycloakSub は JITプロビジョニング用
//
// keycloak_sub で既存レコードを探し、無ければ作成、あれば name/email/role を最新の IDトークン内容に同期する
//
// 【セクション16.2で変更】
// keycloak_sub は users ではなく user_keycloaks テーブルにあるため、
// JOIN経由で検索し、新規作成時は users/user_keycloaks の2テーブルへトランザクションでINSERTする
//
// Rails対比: Railsの `find_or_initialize_by` + `assign_attributes` + `save!` に相当
func (r *User) UpsertByKeycloakSub(ctx context.Context, keycloakSub, email, name string, role model.Role) (*model.User, error) {
	var user model.User
	err := r.db.WithContext(ctx).
		Joins("JOIN user_keycloaks ON user_keycloaks.user_id = users.id").
		Where("user_keycloaks.keycloak_sub = ?", keycloakSub).
		First(&user).Error
	switch {
	case err == nil:
		user.Email = email
		user.Name = name
		// 既にmanagement権限を持つユーザーをKeycloak側の同期不足でgeneralへ
		// 誤って降格させない(CONTRACT.md: role同期は初期反映のみのスコープ)よう、
		// 既存がmanagementなら維持し、general→managementの昇格のみ反映する
		if role == model.RoleManagement {
			user.Role = model.RoleManagement
		}
		if err := r.db.WithContext(ctx).Save(&user).Error; err != nil {
			return nil, fmt.Errorf("ユーザー更新(keycloak_sub=%s): %w", keycloakSub, err)
		}
		return &user, nil
	case errors.Is(err, gorm.ErrRecordNotFound):
		err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			user = model.User{Email: email, Name: name, Role: role}
			if err := tx.Create(&user).Error; err != nil {
				return err
			}
			uk := model.UserKeycloak{UserID: user.ID, KeycloakSub: keycloakSub}
			if err := tx.Create(&uk).Error; err != nil {
				return err
			}
			return nil
		})
		if err != nil {
			// 【4回目のテスト監査(ワイヤー契約パリティ・並行性の新角度)で発覚・修正】
			// このSELECT→CREATEは原子的なUPSERTではないため、同じKeycloak subに対する
			// 2つのリクエストがほぼ同時に届くと、両方とも直前のSELECTで「まだ居ない」を
			// 見た直後にそれぞれCREATEを試み、後勝ちの一方がusers.email/
			// user_keycloaks.keycloak_subのUNIQUE制約違反で失敗していた(実際に10並行リクエストで
			// 再現・確認済み、test/integration/user_jit_provision_race_test.go参照)。
			// isDuplicateEmailError(service/user.go)と同じ「重複エラーはenqueueされた
			// 別リクエストが先に成功した証拠」という考え方で、この場合は失敗ではなく
			// 「既に作成された行を読み直して返す」のが正しい(Rails版find_or_initialize_byの
			// 実質的な原子性に合わせる)
			if isDuplicateUserRaceError(err) {
				var existing model.User
				if selErr := r.db.WithContext(ctx).
					Joins("JOIN user_keycloaks ON user_keycloaks.user_id = users.id").
					Where("user_keycloaks.keycloak_sub = ?", keycloakSub).
					First(&existing).Error; selErr == nil {
					return &existing, nil
				}
			}
			return nil, fmt.Errorf("ユーザー作成(keycloak_sub=%s): %w", keycloakSub, err)
		}
		return &user, nil
	default:
		return nil, fmt.Errorf("ユーザー検索(keycloak_sub=%s): %w", keycloakSub, err)
	}
}

// GetByKeycloakSub はJWTの `sub` クレームからアプリ内部のユーザー行を特定する
// v1(REST)・v2(gRPC)いずれのAPIも、BFFから転送されたAccess Tokenのsubだけを信頼し、
// ここで解決した内部ID(uint64)をTask等のuser_id外部キーとして使う
// (BFFが渡す値を直接信頼せず、必ずJWT検証後のsubから引き直す設計)
//
// 【セクション16.5で変更】Keycloak発行のJWTに限った経路であり、ローカル
// (HMAC/RSA)発行のJWTはsubに内部user_idそのものが入るため別経路
// (repository.User.Get)で解決する(handler/v1/task.go, grpcserver/task_service.go参照)
func (r *User) GetByKeycloakSub(ctx context.Context, keycloakSub string) (*model.User, error) {
	var user model.User
	if err := r.db.WithContext(ctx).
		Joins("JOIN user_keycloaks ON user_keycloaks.user_id = users.id").
		Where("user_keycloaks.keycloak_sub = ?", keycloakSub).
		First(&user).Error; err != nil {
		return nil, err
	}
	return &user, nil
}

func (r *User) Get(ctx context.Context, id uint64) (*model.User, error) {
	var user model.User
	if err := r.db.WithContext(ctx).First(&user, id).Error; err != nil {
		return nil, err
	}
	return &user, nil
}

func (r *User) List(ctx context.Context) ([]model.User, error) {
	var users []model.User
	if err := r.db.WithContext(ctx).Order("id ASC").Find(&users).Error; err != nil {
		return nil, fmt.Errorf("ユーザー一覧取得: %w", err)
	}
	return users, nil
}

// ErrLastManagerUser は「最後の管理者を降格・削除しようとした」ことを表す
// repository層のセンチネル
//
// service.ErrLastManagerUserとは別物として定義している
//
// repositoryがserviceをimportするとimportサイクルになるため
//
// service の UpdateRole/Delete 側で errors.Is して自分の ErrLastManagerUser へ翻訳する
// isDuplicateEmailError と同じ「repository層の事情をservice層が知って翻訳する」既存パターンを踏襲する
var ErrLastManagerUser = errors.New("最後の管理者です")

// checkNotLastManagerLocked は「対象ユーザーがmanagementで、かつ現在の management人数が
// 1人以下なら拒否する」ガードを、渡されたtx(トランザクション)の中で行う
//
// 【2回目のテスト監査で発覚した実バグ(TOCTOUレース)】
// 以前は service 層が「Get→CountManagement(参照)→UpdateRole/Delete(更新)」を別々の非トランザクションなクエリとして呼んでいた
//
// management が残り2人のとき、2つの降格/削除リクエストがほぼ同時に届くと、両方とも「今はまだ2人いる」という参照結果を見た直後にそれぞれ更新を実行してしまい、
// 両方成功して management が0人になる(=誰もFeature Flag/ユーザー管理画面を操作できなくなり、DBを直接触るしか復旧手段が無くなる)、という実際に再現できる競合状態だった
//
// 対策として、参照(COUNT)に MySQL の `SELECT ... FOR UPDATE` 行ロックを付け、同じ management 行の集合を見ようとする2つ目のトランザクションを1つ目の COMMIT まで
// 待たせることで、この競合を防ぐ
//
// トランザクション分離レベルの力を借りる、「先にチェックしてから後で更新する」を「1つの読み書きとして直列化する」という標準的なTOCTOU対策
func (r *User) checkNotLastManagerLocked(tx *gorm.DB, id uint64) error {
	var target model.User
	if err := tx.First(&target, id).Error; err != nil {
		return err
	}
	if target.Role != model.RoleManagement {
		return nil
	}
	var count int64
	if err := tx.Model(&model.User{}).
		Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("role = ?", model.RoleManagement).
		Count(&count).Error; err != nil {
		return fmt.Errorf("management件数取得: %w", err)
	}
	if count <= 1 {
		return ErrLastManagerUser
	}
	return nil
}

// UpdateRoleGuarded はAdmin::Users相当のrole変更
// 「最後の管理者をgeneralに変更できない」ガードと実際の更新を同一トランザクション内で行う
//
// checkNotLastManagerLocked のコメント参照
//
// general 以外への変更(降格ではない)はそもそも管理者を減らさないためガード自体を行わない
func (r *User) UpdateRoleGuarded(ctx context.Context, id uint64, role model.Role) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if role == model.RoleGeneral {
			if err := r.checkNotLastManagerLocked(tx, id); err != nil {
				return err
			}
		}
		result := tx.Model(&model.User{}).Where("id = ?", id).Update("role", role)
		if result.Error != nil {
			return fmt.Errorf("ユーザー(id=%d)のrole更新: %w", id, result.Error)
		}
		return nil
	})
}

// CountManagement は現在management権限を持つユーザー数を数える
//
// admin一覧表示等、ガード以外の参照目的で使う
// ガード自体はcheckNotLastManagerLocked経由で行うため、こちらはロック無しの素朴なCOUNTのままでよい
func (r *User) CountManagement(ctx context.Context) (int64, error) {
	var count int64
	if err := r.db.WithContext(ctx).Model(&model.User{}).Where("role = ?", model.RoleManagement).Count(&count).Error; err != nil {
		return 0, fmt.Errorf("management件数取得: %w", err)
	}
	return count, nil
}

// Create はadmin/go・admin/rails向けのローカル認証ユーザー新規作成(CONTRACT.mdセクション17.2)
// users.emailにはUNIQUE制約(migrations/000001)があるため、重複時はそのままGORMのエラーを
// 返す(呼び出し元のservice層がMySQLのDuplicate entryエラーかどうかで判別する)
func (r *User) Create(ctx context.Context, name, email, passwordDigest string, role model.Role, passwordExpiresAt time.Time) (*model.User, error) {
	var user model.User
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		user = model.User{Email: email, Name: name, Role: role}
		if err := tx.Create(&user).Error; err != nil {
			return err
		}
		pw := model.UserPassword{
			UserID:            user.ID,
			PasswordDigest:    passwordDigest,
			PasswordExpiresAt: passwordExpiresAt,
		}
		if err := tx.Create(&pw).Error; err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &user, nil
}

// Delete は user_passwords/user_keycloaks(いずれもuser_id への FK制約あり、
// migrations/000008)を先に削除してから、Rails版の `has_many :tasks,
// dependent: :destroy`(model/user.go参照)にならい、対象ユーザーの task_labels/tasks も削除したうえでusersを削除する
//
// tasks/task_labels/webauthn_credentialsにはFK制約自体は無いが、削除せず放置すると
// 「存在しないuser_idを指す行」という不整合データが残るため、Rails版の挙動に合わせて明示的にカスケード削除する
//
// 【テスト監査で発見・修正した実バグ】webauthn_credentials(migrations/000013、パスキー機能)は
// この Delete が書かれた後に追加されたテーブルで、削除対象から漏れていた。パスキー登録済みの
// ユーザーを削除すると、存在しないuser_idを指すwebauthn_credentials行が永久に残っていた
// (users.idはAUTO_INCREMENTで削除後に再利用されないため実害は限定的だが、データ不整合ではある)
func (r *User) Delete(ctx context.Context, id uint64) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 【2回目のテスト監査で追加】UpdateRoleGuardedと同じTOCTOU対策
		// (checkNotLastManagerLockedのコメント参照)
		//
		// 削除は常にガード対象(降格と違い
		// 「削除しない」という選択肢が無いため、UpdateRoleGuardedのようなroleでの
		// 分岐は無く、対象がmanagementなら常にチェックする)
		if err := r.checkNotLastManagerLocked(tx, id); err != nil {
			return err
		}
		if err := tx.Where("user_id = ?", id).Delete(&model.UserPassword{}).Error; err != nil {
			return fmt.Errorf("user_passwords削除(user_id=%d): %w", id, err)
		}
		if err := tx.Where("user_id = ?", id).Delete(&model.UserKeycloak{}).Error; err != nil {
			return fmt.Errorf("user_keycloaks削除(user_id=%d): %w", id, err)
		}
		if err := tx.Where("task_id IN (?)", tx.Model(&model.Task{}).Select("id").Where("user_id = ?", id)).
			Delete(&model.TaskLabel{}).Error; err != nil {
			return fmt.Errorf("task_labels削除(user_id=%d): %w", id, err)
		}
		if err := tx.Where("user_id = ?", id).Delete(&model.Task{}).Error; err != nil {
			return fmt.Errorf("tasks削除(user_id=%d): %w", id, err)
		}
		if err := tx.Where("user_id = ?", id).Delete(&model.WebauthnCredential{}).Error; err != nil {
			return fmt.Errorf("webauthn_credentials削除(user_id=%d): %w", id, err)
		}
		result := tx.Delete(&model.User{}, id)
		if result.Error != nil {
			return fmt.Errorf("users削除(id=%d): %w", id, result.Error)
		}
		if result.RowsAffected == 0 {
			return gorm.ErrRecordNotFound
		}
		return nil
	})
}
