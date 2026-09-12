package v1

import (
	"context"
	"encoding/base64"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/masahikokawai/training-go/bff-gin/backend/internal/authjwt"
	"github.com/masahikokawai/training-go/bff-gin/backend/internal/service"
)

// webauthnCredentialService はWebauthnHandlerが必要とする最小のservice操作
type webauthnCredentialService interface {
	Register(ctx context.Context, in service.RegisterWebauthnCredentialInput) (uint64, error)
	FindByCredentialID(ctx context.Context, credentialID []byte) (service.WebauthnCredentialDTO, error)
	UpdateSignCount(ctx context.Context, credentialID []byte, signCount uint64) error
}

// WebauthnHandler は /internal/v1/auth/webauthn/credentials* を提供する(CONTRACT.mdセクション22.4)
// go-webauthnライブラリ自体はbff側が使うため、ここは検証済みの値をそのまま保存/参照するだけの層
type WebauthnHandler struct {
	credentials webauthnCredentialService
	users       userLookup
}

func NewWebauthnHandler(credentials webauthnCredentialService, users userLookup) *WebauthnHandler {
	return &WebauthnHandler{credentials: credentials, users: users}
}

// resolveUserID はJWT検証済みclaimsのsubから内部ユーザーIDを引く
// task.goのTaskHandler.resolveUserIDと全く同じロジック(パッケージ内で共有していないのは
// 既存コードの都合であり、意図的な重複ではない)
func (h *WebauthnHandler) resolveUserID(c *gin.Context) (uint64, bool) {
	claims, ok := authjwt.ClaimsFromContext(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return 0, false
	}

	if authjwt.IsLocalIssuer(claims.Issuer) {
		id, err := strconv.ParseUint(claims.Subject, 10, 64)
		if err != nil {
			c.JSON(http.StatusForbidden, gin.H{"error": "user_not_provisioned"})
			return 0, false
		}
		user, err := h.users.Get(c.Request.Context(), id)
		if err != nil {
			c.JSON(http.StatusForbidden, gin.H{"error": "user_not_provisioned"})
			return 0, false
		}
		return user.ID, true
	}

	user, err := h.users.GetByKeycloakSub(c.Request.Context(), claims.Subject)
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": "user_not_provisioned"})
		return 0, false
	}
	return user.ID, true
}

// credentialIDEncoding はcredential_idの文字列表現(base64url、パディング無し)
// WebAuthn仕様(RFC 4648 base64url、パディング省略)に合わせる
var credentialIDEncoding = base64.RawURLEncoding

type registerWebauthnCredentialRequest struct {
	CredentialID string   `json:"credential_id" binding:"required"`
	PublicKey    string   `json:"public_key" binding:"required"`
	SignCount    uint64   `json:"sign_count"`
	// BackupEligible/BackupState はWebAuthnのBE/BSフラグ(実機デバッグで追加、
	// マイグレーション000015参照)。詳細はservice.RegisterWebauthnCredentialInputのコメント参照
	BackupEligible bool     `json:"backup_eligible"`
	BackupState    bool     `json:"backup_state"`
	Transports     []string `json:"transports"`
	Name           *string  `json:"name"`
}

// Register は POST /internal/v1/auth/webauthn/credentials(通常のRequireAuth配下)ログイン中ユーザーが新しいパスキーを登録する
//
// bff が検証済みの attestation から抽出した credential_id/public_key をそのまま保存するだけで、署名検証自体はbff側が担う
func (h *WebauthnHandler) Register(c *gin.Context) {
	userID, ok := h.resolveUserID(c)
	if !ok {
		return
	}
	var body registerWebauthnCredentialRequest
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request"})
		return
	}
	credentialID, err := credentialIDEncoding.DecodeString(body.CredentialID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_credential_id"})
		return
	}
	publicKey, err := base64.StdEncoding.DecodeString(body.PublicKey)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_public_key"})
		return
	}
	var transports *string
	if len(body.Transports) > 0 {
		joined := strings.Join(body.Transports, ",")
		transports = &joined
	}

	id, err := h.credentials.Register(c.Request.Context(), service.RegisterWebauthnCredentialInput{
		UserID:         userID,
		CredentialID:   credentialID,
		PublicKey:      publicKey,
		SignCount:      body.SignCount,
		BackupEligible: body.BackupEligible,
		BackupState:    body.BackupState,
		Transports:     transports,
		Name:           body.Name,
	})
	if err != nil {
		renderServiceError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"id": id})
}

// credentialIDFromParam はURLパスの`:credential_id`(base64url)を生バイト列へ変換する
func credentialIDFromParam(c *gin.Context) ([]byte, bool) {
	raw := c.Param("credential_id")
	decoded, err := credentialIDEncoding.DecodeString(raw)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_credential_id"})
		return nil, false
	}
	return decoded, true
}

// Get は GET /internal/v1/auth/webauthn/credentials/:credential_id
// (RequireWebauthnInternalToken配下、まだ未認証のログイン試行中にbffが呼ぶ)
// discoverable credentialでのログイン時、返ってきたcredential_idからuser_id・公開鍵・
// sign_countを引くために使う
//
// 【bffとの結合確認で判明・追記】
// bff はこのレスポンスからそのままセッション(name/email/roles)を発行するため、
// user_id・public_key・sign_count だけでなく、users テーブルの name/email/role も一緒に返す必要がある
// 他の箇所(Admin::Users等)と同じく h.users からユーザー行を引いて合流させる
// roles は Keycloak 発行トークンの realm_access.roles(配列) と bff 側で同じ型として扱えるよう、単一の role を要素数1の配列として返す
func (h *WebauthnHandler) Get(c *gin.Context) {
	credentialID, ok := credentialIDFromParam(c)
	if !ok {
		return
	}
	dto, err := h.credentials.FindByCredentialID(c.Request.Context(), credentialID)
	if err != nil {
		renderServiceError(c, err)
		return
	}
	user, err := h.users.Get(c.Request.Context(), dto.UserID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal_server_error"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"user_id":         dto.UserID,
		"credential_id":   credentialIDEncoding.EncodeToString(dto.CredentialID),
		"public_key":      base64.StdEncoding.EncodeToString(dto.PublicKey),
		"sign_count":      dto.SignCount,
		"backup_eligible": dto.BackupEligible,
		"backup_state":    dto.BackupState,
		"name":            user.Name,
		"email":           user.Email,
		"roles":           []string{user.Role.String()},
	})
}

type updateSignCountRequest struct {
	SignCount uint64 `json:"sign_count"`
}

// UpdateSignCount は PATCH /internal/v1/auth/webauthn/credentials/:credential_id/sign-count
// (RequireWebauthnInternalToken配下)
// ログイン成功後、リプレイ攻撃対策のsign_countを更新する
func (h *WebauthnHandler) UpdateSignCount(c *gin.Context) {
	credentialID, ok := credentialIDFromParam(c)
	if !ok {
		return
	}
	var body updateSignCountRequest
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request"})
		return
	}
	if err := h.credentials.UpdateSignCount(c.Request.Context(), credentialID, body.SignCount); err != nil {
		renderServiceError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// RequireWebauthnInternalToken はwebauthnのGET/PATCH(ログイン試行中でJWTを持たない)
// 専用の軽量な認可ミドルウェア
// RequireLocalAuthInternalTokenと全く同じパターン
// (CONTRACT.mdセクション22.4)
func RequireWebauthnInternalToken(expectedToken string) gin.HandlerFunc {
	return func(c *gin.Context) {
		got := c.GetHeader("X-Webauthn-Internal-Token")
		if got == "" || !secureTokenEqual(got, expectedToken) {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			return
		}
		c.Next()
	}
}
