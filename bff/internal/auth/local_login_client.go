package auth

import (
	"context"
	"errors"
	"fmt"

	"github.com/go-resty/resty/v2"
)

// ErrInvalidLocalCredentials はメールアドレス/パスワードの不一致
var ErrInvalidLocalCredentials = errors.New("invalid_credentials")

// ErrLocalPasswordExpired は資格情報自体は正しいが有効期限が切れている場合
// (CONTRACT.mdセクション16.3)
// 「ログアウトさせる」のではなく「そもそもログインさせない」
// 挙動にするため、Sessionが作られる前のこの時点でエラーとして返す
var ErrLocalPasswordExpired = errors.New("password_expired")

// LocalLoginClient はbackendの POST /internal/v1/auth/verify-local-password を呼ぶ
// bffはDBに直接アクセスしない方針(CONTRACT.md冒頭)のため、パスワード照合自体は
// backendに委譲し、bffは結果(成功/資格情報不一致/期限切れ)だけを受け取る
type LocalLoginClient struct {
	http          *resty.Client
	internalToken string
}

func NewLocalLoginClient(verifyPasswordURL, internalToken string) *LocalLoginClient {
	return &LocalLoginClient{
		http:          resty.New().SetBaseURL(verifyPasswordURL),
		internalToken: internalToken,
	}
}

type verifyLocalPasswordResponse struct {
	UserID uint64   `json:"user_id"`
	Name   string   `json:"name"`
	Email  string   `json:"email"`
	Roles  []string `json:"roles"`
	Error  string   `json:"error"`
}

// VerifyPassword はemail/passwordをbackendへ照会する
// baseURLにフルパスを渡している(config.LocalAuthVerifyPasswordURLがURL全体)ため、
// リクエスト自体はパスを付けずbaseURLへそのままPOSTする
func (c *LocalLoginClient) VerifyPassword(ctx context.Context, email, password string) (userID uint64, name, userEmail string, roles []string, err error) {
	var out verifyLocalPasswordResponse
	resp, err := c.http.R().
		SetContext(ctx).
		SetHeader("X-Local-Auth-Internal-Token", c.internalToken).
		SetBody(map[string]string{"email": email, "password": password}).
		SetResult(&out).
		SetError(&out).
		Post("")
	if err != nil {
		return 0, "", "", nil, fmt.Errorf("パスワード照会のリクエストに失敗しました: %w", err)
	}

	switch {
	case resp.StatusCode() == 200:
		return out.UserID, out.Name, out.Email, out.Roles, nil
	case out.Error == "password_expired":
		return 0, "", "", nil, ErrLocalPasswordExpired
	case out.Error == "invalid_credentials":
		return 0, "", "", nil, ErrInvalidLocalCredentials
	default:
		return 0, "", "", nil, fmt.Errorf("パスワード照会が予期しないレスポンスを返しました: status=%d body=%s", resp.StatusCode(), resp.String())
	}
}
