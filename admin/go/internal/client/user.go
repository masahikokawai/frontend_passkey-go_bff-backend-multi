// Package client はbackendの内部APIを叩くHTTPクライアント群
//
// CONTRACT.mdセクション17.1: ユーザー管理はFeature Flag(admin/goがMySQLへ直接GORM接続)とは
// 異なり、backendの `/internal/v1/admin/users` をHTTP経由で叩く設計にしている
// 「最後の管理者を降格/削除できない」という業務ルールをbackend側の service/user.go
// 1箇所にだけ実装し、admin/go・admin/rails双方での重複実装によるズレを防ぐため
package client

import (
	"context"
	"errors"
	"fmt"

	"github.com/go-resty/resty/v2"
)

// User はbackendの /internal/v1/admin/users が返すユーザー1件分
//
// CONTRACT.mdセクション22.7:
// HasPasskeyはwebauthn_credentialsにこのユーザーの登録済み資格情報が1件以上あるかどうか(backend側でEXISTSサブクエリにより判定)
//
// backend側の実装がこのフィールドをまだ返さない古いレスポンスでも、Goのゼロ値により false として扱われるだけでエラーにはならない
type User struct {
	ID         uint64 `json:"id"`
	Name       string `json:"name"`
	Email      string `json:"email"`
	Role       string `json:"role"`
	HasPasskey bool   `json:"has_passkey"`
}

// ErrValidation はbackendが422 かつ error="validation_error" を返した場合
// (email重複・password空・role不正等)
var ErrValidation = errors.New("validation error")

// ErrLastManager はbackendが422 かつ error="last_manager_user" を返した場合
// (最後の管理者を降格/削除しようとした)
// 【実装時に判明】backendの`renderServiceError`はErrLastManagerUserもErrValidationと
// 同じ422を返す設計(local-auth実装のinvalid_credentials/password_expiredと同じく、
// ステータスコードではなくJSONボディの`error`キーで種別を判別する既存パターンに統一)
// 当初409を前提に実装していたが、backend側の実装完了後にこの食い違いが判明し修正した
var ErrLastManager = errors.New("last manager user")

// ErrNotFound はbackendが404を返した場合
var ErrNotFound = errors.New("not found")

// errorResponse はbackendのエラーJSON形状(backend/internal/handler/v1/render.go)
// `error`が種別を判別するキー(例: "validation_error"/"last_manager_user"/"not_found")、
// `message`が実際に画面へ表示すべき人間向けの文言(常に付与されるわけではない)
type errorResponse struct {
	Error   string `json:"error"`
	Message string `json:"message"`
}

// UserClient はbackendの /internal/v1/admin/users 系APIのクライアント
type UserClient struct {
	http *resty.Client
}

func NewUserClient(baseURL, adminInternalToken string) *UserClient {
	return &UserClient{
		http: resty.New().
			SetBaseURL(baseURL).
			SetHeader("X-Admin-Internal-Token", adminInternalToken),
	}
}

type listUsersResponse struct {
	Users []User `json:"users"`
}

// List はユーザー一覧を取得する(Admin::Users一覧相当)
func (c *UserClient) List(ctx context.Context) ([]User, error) {
	var body listUsersResponse
	resp, err := c.http.R().SetContext(ctx).SetResult(&body).Get("/internal/v1/admin/users")
	if err != nil {
		return nil, fmt.Errorf("ユーザー一覧取得のリクエストに失敗しました: %w", err)
	}
	if resp.IsError() {
		return nil, fmt.Errorf("ユーザー一覧取得が失敗しました(status=%d): %s", resp.StatusCode(), resp.String())
	}
	return body.Users, nil
}

// CreateInput は新規ローカル認証ユーザー作成の入力
// CONTRACT.mdセクション17.2: admin画面で作成できるのはローカル認証ユーザーのみ
// (Keycloakユーザーは初回ログイン時のJITプロビジョニングで自動作成されるため)
type CreateInput struct {
	Name     string
	Email    string
	Password string
	Role     string
}

// createResponse はbackendの POST /internal/v1/admin/users のレスポンス形状
// 【実装時に判明】List(GET)は`"id"`キーだが、Create/Provision(backendの既存の
// user_provision.go Provisionと同じ命名規則)は`"user_id"`キーを使う
// backend内でもキー名が統一されていないため、Userとは別の構造体で受ける(Userのjson:"id"タグのまま
// SetResultに渡すと、`user_id`キーと一致せずIDが常にゼロ値になってしまう)
type createResponse struct {
	ID    uint64 `json:"user_id"`
	Name  string `json:"name"`
	Email string `json:"email"`
	Role  string `json:"role"`
}

// Create は新規ローカル認証ユーザーを作成する
func (c *UserClient) Create(ctx context.Context, in CreateInput) (User, error) {
	var created createResponse
	var errBody errorResponse
	resp, err := c.http.R().
		SetContext(ctx).
		SetBody(map[string]string{
			"name":     in.Name,
			"email":    in.Email,
			"password": in.Password,
			"role":     in.Role,
		}).
		SetResult(&created).
		SetError(&errBody).
		Post("/internal/v1/admin/users")
	if err != nil {
		return User{}, fmt.Errorf("ユーザー作成のリクエストに失敗しました: %w", err)
	}
	if resp.StatusCode() == 422 {
		if err := classifyError(resp.StatusCode(), errBody.Error, errBody.Message); err != nil {
			return User{}, err
		}
	}
	if resp.IsError() {
		return User{}, fmt.Errorf("ユーザー作成が失敗しました(status=%d): %s", resp.StatusCode(), resp.String())
	}
	return User{ID: created.ID, Name: created.Name, Email: created.Email, Role: created.Role}, nil
}

// UpdateRole は指定ユーザーのroleを変更する
func (c *UserClient) UpdateRole(ctx context.Context, id uint64, role string) error {
	var errBody errorResponse
	resp, err := c.http.R().
		SetContext(ctx).
		SetBody(map[string]string{"role": role}).
		SetError(&errBody).
		Patch(fmt.Sprintf("/internal/v1/admin/users/%d/role", id))
	if err != nil {
		return fmt.Errorf("role変更のリクエストに失敗しました: %w", err)
	}
	return classifyError(resp.StatusCode(), errBody.Error, errBody.Message)
}

// Delete は指定ユーザーを削除する
func (c *UserClient) Delete(ctx context.Context, id uint64) error {
	var errBody errorResponse
	resp, err := c.http.R().
		SetContext(ctx).
		SetError(&errBody).
		Delete(fmt.Sprintf("/internal/v1/admin/users/%d", id))
	if err != nil {
		return fmt.Errorf("ユーザー削除のリクエストに失敗しました: %w", err)
	}
	return classifyError(resp.StatusCode(), errBody.Error, errBody.Message)
}

// classifyError はbackendのステータスコードとJSONボディの`error`キーからGoのエラー型を判別する
// 422は`ErrLastManagerUser`(error="last_manager_user")と汎用バリデーションエラー
// (error="validation_error")の両方で使われるため、ステータスコード単独では区別できない
// (backend/internal/handler/v1/render.go参照)
func classifyError(status int, errorKey, message string) error {
	switch status {
	case 200, 201, 204:
		return nil
	case 404:
		return ErrNotFound
	case 422:
		if errorKey == "last_manager_user" {
			return fmt.Errorf("%w: %s", ErrLastManager, message)
		}
		return fmt.Errorf("%w: %s", ErrValidation, message)
	default:
		return fmt.Errorf("backendが予期しないstatus=%dを返しました: %s", status, message)
	}
}
