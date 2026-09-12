// Package v1 はREST v1(旧実装、CONTRACT.md参照)のGinハンドラ群
// backendはBFF以外に公開されない内部APIのため、Railsのようなflash/ERBは無く、
// 素直にJSONを返すだけでよい
package v1

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/masahikokawai/training-go/bff-gin/backend/internal/service"
)

// logger はrenderServiceErrorが internal_server_error を返す際に、原因をサーバー側ログへ残すためのパッケージ変数
// 以前はここでエラーを握りつぶしており、
// 500の原因が全く分からなかった(cmd/server/main.goのSetLogger呼び出し参照)
var logger *slog.Logger = slog.Default()

// SetLogger はmain.goから起動時に一度だけ呼ぶ
func SetLogger(l *slog.Logger) {
	logger = l
}

// renderServiceError はservice層のSentinel Errorを適切なHTTPステータスへ変換する
func renderServiceError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": "not_found"})
	case errors.Is(err, service.ErrValidation):
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": "validation_error", "message": err.Error()})
	case errors.Is(err, service.ErrForbidden):
		c.JSON(http.StatusForbidden, gin.H{"error": "forbidden"})
	case errors.Is(err, service.ErrLastManagerUser):
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": "last_manager_user", "message": err.Error()})
	default:
		logger.Error("internal_server_error",
			slog.String("path", c.Request.URL.Path),
			slog.Any("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal_server_error"})
	}
}
