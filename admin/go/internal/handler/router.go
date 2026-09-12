package handler

import (
	"github.com/gin-gonic/gin"
)

// NewRouter はGinのルーターを組み立てる
// 全ルートにHTTP Basic Authを掛ける
// (CONTRACT.mdセクション13: OIDCは導入せず、この管理アプリでは意図的に簡略化している)
func NewRouter(h *FeatureFlagHandler, u *UserHandler, basicAuthUser, basicAuthPassword string) *gin.Engine {
	router := gin.New()
	router.Use(gin.Recovery())
	router.Use(gin.BasicAuth(gin.Accounts{
		basicAuthUser: basicAuthPassword,
	}))

	router.GET("/", h.Index)
	router.GET("/flags/:id/edit", h.Edit)
	router.POST("/flags/:id", h.Update)
	router.GET("/flags/:id/audit_log", h.AuditLog)

	// ユーザー管理(CONTRACT.mdセクション17)
	router.GET("/users", u.Index)
	router.POST("/users", u.Create)
	router.POST("/users/:id/role", u.UpdateRole)
	router.POST("/users/:id/delete", u.Delete)

	return router
}
