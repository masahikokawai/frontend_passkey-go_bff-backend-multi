package v1

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/masahikokawai/training-go/bff-gin/backend/internal/featureflag"
	"github.com/masahikokawai/training-go/bff-gin/backend/internal/repository"
)

// FeatureFlagExportHandler は GET /internal/v1/feature-flags/export を提供する
// bffのGO Feature Flag HTTP retrieverがこのエンドポイントを定期ポーリングする
// (CONTRACT.mdセクション13)
type FeatureFlagExportHandler struct {
	repo *repository.FeatureFlag
}

func NewFeatureFlagExportHandler(repo *repository.FeatureFlag) *FeatureFlagExportHandler {
	return &FeatureFlagExportHandler{repo: repo}
}

func (h *FeatureFlagExportHandler) Export(c *gin.Context) {
	flags, err := h.repo.List(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal_server_error"})
		return
	}
	body, err := featureflag.BuildFlagConfigJSON(flags)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal_server_error"})
		return
	}
	c.Data(http.StatusOK, "application/json; charset=utf-8", body)
}

// RequireFeatureFlagPollToken はexportエンドポイント専用の軽量な認可ミドルウェア
//
// このエンドポイントはユーザーセッションに紐づかない定期ポーリング(bffのGO Feature Flag
// HTTP retrieverがバックグラウンドタイマーで叩く)のため、通常のRequireAuth(JWT検証)は
// 使えない(ログイン中ユーザーのAccess Tokenという概念がそもそも存在しないため)
// そのため、bff/backend双方に同じ値を設定する共有シークレット
// (`X-Feature-Flag-Poll-Token`ヘッダ、環境変数FEATURE_FLAG_POLL_TOKEN)で
// 認可する、通常のJWT検証とは別方式にしている
func RequireFeatureFlagPollToken(expectedToken string) gin.HandlerFunc {
	return func(c *gin.Context) {
		got := c.GetHeader("X-Feature-Flag-Poll-Token")
		if got == "" || !secureTokenEqual(got, expectedToken) {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			return
		}
		c.Next()
	}
}
