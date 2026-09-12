package auth

import (
	"context"
	"errors"
	"fmt"

	"github.com/go-resty/resty/v2"
)

// ErrWebauthnCredentialNotFound は credential_id に対応する行が backend に無い場合
var ErrWebauthnCredentialNotFound = errors.New("webauthn_credential_not_found")

// WebauthnCredentialStore はbackendの新規内部API(CONTRACT.mdセクション22.4)を呼ぶ
// bff は DB に直接アクセスしない方針(CONTRACT.md冒頭)のため、webauthn_credentials テーブルの読み書きは backend に委譲する
type WebauthnCredentialStore interface {
	// Register はログイン中ユーザーの新しいパスキーを永続化する(要access token、通常のRequireAuth)
	// backupEligible/backupState はWebAuthnのBE/BSフラグ(実機デバッグで追加)。
	// 登録時に記録した値をログイン時の検証でも使い直さないと、go-webauthnが
	// 「登録時と申告内容が矛盾している」として正当なログインを拒否してしまう
	// (クラウド同期されるパスキー(iCloudキーチェーン・Googleパスワードマネージャー等)は
	// BackupEligible=trueを申告するため、これを保存し損ねると必ずこの拒否が起きる)
	Register(ctx context.Context, accessToken, credentialID, publicKey string, signCount uint32, backupEligible, backupState bool, transports []string, name string) error
	// Lookup はcredential_idから、公開鍵・sign_count・BE/BSフラグに加えセッション発行に
	// 必要なname/email/rolesまで一括で引く(ログイン試行中はまだJWTが無く、他に「特定の
	// user_idのプロフィールを取得する」内部APIが存在しないため、この照会1本でセッション発行に必要な情報を全て揃える設計にした
	// CONTRACT.mdセクション22.4の当初案(user_id/public_key/sign_countのみ)を拡張している)
	Lookup(ctx context.Context, credentialID string) (userID uint64, name, email string, roles []string, publicKey string, signCount uint32, backupEligible, backupState bool, err error)
	// UpdateSignCount はログイン成功後、リプレイ攻撃対策のsign_countを更新する(同上ヘッダ)
	UpdateSignCount(ctx context.Context, credentialID string, signCount uint32) error
}

// WebauthnBackendClient はWebauthnCredentialStoreの実装
type WebauthnBackendClient struct {
	http          *resty.Client
	internalToken string
}

func NewWebauthnBackendClient(baseURL, internalToken string) *WebauthnBackendClient {
	return &WebauthnBackendClient{
		http:          resty.New().SetBaseURL(baseURL),
		internalToken: internalToken,
	}
}

type webauthnCredentialResponse struct {
	UserID         uint64   `json:"user_id"`
	Name           string   `json:"name"`
	Email          string   `json:"email"`
	Roles          []string `json:"roles"`
	PublicKey      string   `json:"public_key"`
	SignCount      uint32   `json:"sign_count"`
	BackupEligible bool     `json:"backup_eligible"`
	BackupState    bool     `json:"backup_state"`
	Error          string   `json:"error"`
}

func (c *WebauthnBackendClient) Register(ctx context.Context, accessToken, credentialID, publicKey string, signCount uint32, backupEligible, backupState bool, transports []string, name string) error {
	resp, err := c.http.R().
		SetContext(ctx).
		SetAuthToken(accessToken).
		SetBody(map[string]any{
			"credential_id":   credentialID,
			"public_key":      publicKey,
			"sign_count":      signCount,
			"backup_eligible": backupEligible,
			"backup_state":    backupState,
			"transports":      transports,
			"name":            name,
		}).
		Post("/internal/v1/auth/webauthn/credentials")
	if err != nil {
		return fmt.Errorf("パスキー登録のリクエストに失敗しました: %w", err)
	}
	if resp.StatusCode() != 201 {
		return fmt.Errorf("パスキー登録がbackendで失敗しました: status=%d body=%s", resp.StatusCode(), resp.String())
	}
	return nil
}

func (c *WebauthnBackendClient) Lookup(ctx context.Context, credentialID string) (userID uint64, name, email string, roles []string, publicKey string, signCount uint32, backupEligible, backupState bool, err error) {
	var out webauthnCredentialResponse
	resp, reqErr := c.http.R().
		SetContext(ctx).
		SetHeader("X-Webauthn-Internal-Token", c.internalToken).
		SetResult(&out).
		SetError(&out).
		Get("/internal/v1/auth/webauthn/credentials/" + credentialID)
	if reqErr != nil {
		return 0, "", "", nil, "", 0, false, false, fmt.Errorf("パスキー照会のリクエストに失敗しました: %w", reqErr)
	}
	switch resp.StatusCode() {
	case 200:
		return out.UserID, out.Name, out.Email, out.Roles, out.PublicKey, out.SignCount, out.BackupEligible, out.BackupState, nil
	case 404:
		return 0, "", "", nil, "", 0, false, false, ErrWebauthnCredentialNotFound
	default:
		return 0, "", "", nil, "", 0, false, false, fmt.Errorf("パスキー照会が予期しないレスポンスを返しました: status=%d body=%s", resp.StatusCode(), resp.String())
	}
}

func (c *WebauthnBackendClient) UpdateSignCount(ctx context.Context, credentialID string, signCount uint32) error {
	resp, err := c.http.R().
		SetContext(ctx).
		SetHeader("X-Webauthn-Internal-Token", c.internalToken).
		SetBody(map[string]any{"sign_count": signCount}).
		Patch("/internal/v1/auth/webauthn/credentials/" + credentialID + "/sign-count")
	if err != nil {
		return fmt.Errorf("sign_count更新のリクエストに失敗しました: %w", err)
	}
	if resp.StatusCode() != 204 {
		return fmt.Errorf("sign_count更新がbackendで失敗しました: status=%d body=%s", resp.StatusCode(), resp.String())
	}
	return nil
}
