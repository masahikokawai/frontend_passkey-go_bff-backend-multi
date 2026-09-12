package proxy

import (
	"context"
	"fmt"

	"github.com/go-resty/resty/v2"
)

// UserProvisionClient はbackendへJIT(Just-In-Time)プロビジョニングを依頼する
// CONTRACT.mdセクション10・5.1参照
// auth.UserProvisioner interfaceを満たす
type UserProvisionClient struct {
	http *resty.Client
}

func NewUserProvisionClient(baseURL string) *UserProvisionClient {
	return &UserProvisionClient{http: resty.New().SetBaseURL(baseURL)}
}

type provisionRequestBody struct {
	KeycloakSub string   `json:"keycloak_sub"`
	Name        string   `json:"name"`
	Email       string   `json:"email"`
	Roles       []string `json:"roles"`
}

type provisionResponseBody struct {
	UserID uint64 `json:"user_id"`
	Role   string `json:"role"`
}

// Provision はauth.UserProvisioner interfaceの実装
// 注: backendの/internal/v1/users/provisionは他の全エンドポイントと同じくRequireAuth
// ミドルウェア配下にあり、身元はリクエストボディではなくBearerトークンのJWTクレームから backend が自分で解決する
// (bodyは送っても無視される CONTRACT.md/backend 実装を参照)
// このコール時点ではまだRedisセッションが存在しないため、トークン交換直後のaccessTokenを
// そのまま使う(リアクティブリフレッシュの対象外)
func (c *UserProvisionClient) Provision(ctx context.Context, accessToken, keycloakSub, name, email string, roles []string) (uint64, string, error) {
	var out provisionResponseBody
	resp, err := c.http.R().
		SetContext(ctx).
		SetAuthToken(accessToken).
		SetBody(provisionRequestBody{KeycloakSub: keycloakSub, Name: name, Email: email, Roles: roles}).
		SetResult(&out).
		Post("/internal/v1/users/provision")
	if err != nil {
		return 0, "", fmt.Errorf("JITプロビジョニングのリクエストに失敗しました: %w", err)
	}
	if resp.IsError() {
		return 0, "", fmt.Errorf("JITプロビジョニングがエラーを返しました: status=%d body=%s", resp.StatusCode(), resp.String())
	}
	return out.UserID, out.Role, nil
}
