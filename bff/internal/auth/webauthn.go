// Package auth: CONTRACT.mdセクション22(パスキー/WebAuthnの追加)
//
// 対象は bff のローカル認証(HMAC/RSA)ユーザーへの追加の認証手段のみ(Keycloak発行
// ユーザー・frontend-railsは対象外、22.1参照)。discoverable credential(resident key)
// 方式を採用し、ログイン画面でメールアドレス等の事前入力は不要にする。
//
// クロスデバイス認証(QRコード→スマホでスキャン→スマホでの生体認証→PC側ブラウザへ結果が
// 戻る)はブラウザ/OSの標準機能(WebAuthnのhybrid transport)であり、bff/frontendどちらも
// これを直接実装する必要はない。navigator.credentials.get()を正しく呼ぶだけでよい。
package auth

import (
	"encoding/base64"
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
)

// NewWebAuthn はgo-webauthnのインスタンスを構築する(CONTRACT.mdセクション22.2)
func NewWebAuthn(rpID, rpDisplayName, rpOrigin string) (*webauthn.WebAuthn, error) {
	return webauthn.New(&webauthn.Config{
		RPID:          rpID,
		RPDisplayName: rpDisplayName,
		RPOrigins:     []string{rpOrigin},
	})
}

// simpleWebauthnUser はwebauthn.Userインターフェースの最小実装
// registerBegin/Finishではログイン中セッションのuser_id/name/emailをそのまま詰めるだけ、
// loginFinishではDiscoverableUserHandler内でbackend照会結果を詰める(credentialsは
// 検証対象の1件だけを持たせれば良い。go-webauthnがこの中からcredential_id一致するものを
// 探して署名検証するため)。userIDはFinishPasskeyLogin後にHandler側で取り出して使う
type simpleWebauthnUser struct {
	userID      uint64
	name        string
	email       string
	roles       []string
	displayName string
	credentials []webauthn.Credential
}

func (u *simpleWebauthnUser) WebAuthnID() []byte {
	return []byte(strconv.FormatUint(u.userID, 10))
}
func (u *simpleWebauthnUser) WebAuthnName() string        { return u.email }
func (u *simpleWebauthnUser) WebAuthnDisplayName() string { return u.displayName }
func (u *simpleWebauthnUser) WebAuthnCredentials() []webauthn.Credential {
	return u.credentials
}

// webauthnRegSessionKey / webauthnLoginSessionKeyPrefix はWebauthnChallengeStoreの
// キー名前空間を登録用/ログイン用で分けるための接頭辞
func webauthnRegSessionKey(sessionID string) string { return "reg:" + sessionID }
func webauthnLoginSessionKey(state string) string   { return "login:" + state }

// requireLocalAuthSession はCONTRACT.mdセクション22.1の対象範囲(ローカル認証ユーザーのみ)を
// 強制する。Keycloak発行セッションでのパスキー登録はスコープ外のため拒否する
func requireLocalAuthSession(c *gin.Context) (*Session, string, bool) {
	sess, sessionID, ok := CurrentSession(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthenticated"})
		return nil, "", false
	}
	if sess.AuthMode != AuthModeLocalHMAC && sess.AuthMode != AuthModeLocalRSA {
		c.JSON(http.StatusForbidden, gin.H{"error": "webauthn_scope_local_auth_only"})
		return nil, "", false
	}
	return sess, sessionID, true
}

// WebauthnRegisterBegin は `POST /api/auth/passkey/register/begin`(要ログイン)
func (h *Handler) WebauthnRegisterBegin(c *gin.Context) {
	sess, sessionID, ok := requireLocalAuthSession(c)
	if !ok {
		return
	}

	user := &simpleWebauthnUser{userID: sess.UserID, name: sess.Name, email: sess.Email, displayName: sess.Name}
	creation, sessionData, err := h.WebAuthn.BeginRegistration(
		user,
		webauthn.WithResidentKeyRequirement(protocol.ResidentKeyRequirementRequired),
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if err := h.WebauthnChallenge.Save(c.Request.Context(), webauthnRegSessionKey(sessionID), sessionData); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, creation)
}

// WebauthnRegisterFinish は `POST /api/auth/passkey/register/finish`(要ログイン)
// リクエストボディは navigator.credentials.create() の結果をそのまま
// (PublicKeyCredential#toJSON())渡す想定のため、ここでは他のフィールドをボディから
// 読まない(端末名を付けたい場合は `?name=` クエリで渡す)
func (h *Handler) WebauthnRegisterFinish(c *gin.Context) {
	sess, sessionID, ok := requireLocalAuthSession(c)
	if !ok {
		return
	}

	sessionData, err := h.WebauthnChallenge.Take(c.Request.Context(), webauthnRegSessionKey(sessionID))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "challenge_expired_or_not_found"})
		return
	}

	user := &simpleWebauthnUser{userID: sess.UserID, name: sess.Name, email: sess.Email, displayName: sess.Name}
	credential, err := h.WebAuthn.FinishRegistration(user, *sessionData, c.Request)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "attestation_verification_failed"})
		return
	}

	credentialID := base64.RawURLEncoding.EncodeToString(credential.ID)
	publicKey := base64.StdEncoding.EncodeToString(credential.PublicKey)
	transports := make([]string, 0, len(credential.Transport))
	for _, t := range credential.Transport {
		transports = append(transports, string(t))
	}
	name := c.Query("name")

	if err := h.WebauthnBackend.Register(c.Request.Context(), sess.AccessToken, credentialID, publicKey, credential.Authenticator.SignCount,
		credential.Flags.BackupEligible, credential.Flags.BackupState, transports, name); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"registered": true})
}

// webauthnLoginBeginResponse はprotocol.CredentialAssertionに、Redisで一時保存した
// challengeを引き直すためのstate(未ログイン状態のためCookieに頼れない)を足したもの
type webauthnLoginBeginResponse struct {
	PublicKey protocol.PublicKeyCredentialRequestOptions `json:"publicKey"`
	State     string                                     `json:"state"`
}

// WebauthnLoginBegin は `POST /api/auth/passkey/login/begin`(未ログインで呼べる)
// discoverable credential方式のため、allowCredentialsを指定しない
// (ブラウザ/OSが端末上の利用可能なパスキーを提示する。CONTRACT.mdセクション22.2)
func (h *Handler) WebauthnLoginBegin(c *gin.Context) {
	assertion, sessionData, err := h.WebAuthn.BeginDiscoverableLogin()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	state, err := randomToken(24)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if err := h.WebauthnChallenge.Save(c.Request.Context(), webauthnLoginSessionKey(state), sessionData); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, webauthnLoginBeginResponse{PublicKey: assertion.Response, State: state})
}

// WebauthnLoginFinish は `POST /api/auth/passkey/login/finish?state=...`(未ログインで呼べる)
// stateはクエリで受け取る(リクエストボディはnavigator.credentials.get()の結果を
// そのまま渡す必要があり、go-webauthnがc.Requestから直接パースするため、ボディに
// 独自フィールドを混ぜられない)
func (h *Handler) WebauthnLoginFinish(c *gin.Context) {
	state := c.Query("state")
	if state == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "state is required"})
		return
	}
	sessionData, err := h.WebauthnChallenge.Take(c.Request.Context(), webauthnLoginSessionKey(state))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "challenge_expired_or_not_found"})
		return
	}

	ctx := c.Request.Context()
	handler := func(rawID, _ []byte) (webauthn.User, error) {
		credentialID := base64.RawURLEncoding.EncodeToString(rawID)
		userID, name, email, roles, publicKeyB64, signCount, backupEligible, backupState, lookupErr := h.WebauthnBackend.Lookup(ctx, credentialID)
		if lookupErr != nil {
			return nil, lookupErr
		}
		publicKey, decErr := base64.StdEncoding.DecodeString(publicKeyB64)
		if decErr != nil {
			return nil, decErr
		}
		return &simpleWebauthnUser{
			userID: userID, name: name, email: email, roles: roles, displayName: name,
			credentials: []webauthn.Credential{{
				ID:            rawID,
				PublicKey:     publicKey,
				Authenticator: webauthn.Authenticator{SignCount: signCount},
				// 【実機デバッグで追加】登録時に記録したBE/BSフラグを、ログイン時の
				// credential再構築でも使い直す。ここを空(ゼロ値)のままにすると、
				// go-webauthnが「登録時はfalseだったのに今回はtrueを申告している」と
				// 誤検知し、"Backup Eligible flag inconsistency detected"で
				// 正当なログインを拒否してしまう(実際に踏んだ不具合)
				Flags: webauthn.CredentialFlags{
					BackupEligible: backupEligible,
					BackupState:    backupState,
				},
			}},
		}, nil
	}

	validatedUser, credential, err := h.WebAuthn.FinishPasskeyLogin(handler, *sessionData, c.Request)
	if err != nil {
		// 【実機デバッグで追記】go-webauthnの検証エラーはこれまでクライアントに汎用的な
		// JSONを返すだけでbff側に一切ログを残しておらず、実機での原因調査ができなかった。
		// go-webauthnの検証系エラーは*protocol.Error(Type/Details/DevInfo/Err)で
		// 返ってくることが多く、特にDevInfoに最も具体的な原因が入っているため、
		// 型アサーションできた場合はそちらも個別に出す
		if h.Logger != nil {
			var perr *protocol.Error
			if errors.As(err, &perr) {
				h.Logger.Warn("パスキーログインの検証に失敗しました",
					slog.String("type", perr.Type),
					slog.String("details", perr.Details),
					slog.String("dev_info", perr.DevInfo),
					slog.Any("inner_error", perr.Err))
			} else {
				h.Logger.Warn("パスキーログインの検証に失敗しました", slog.Any("error", err))
			}
		}
		if errors.Is(err, ErrWebauthnCredentialNotFound) {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "credential_not_found"})
			return
		}
		c.JSON(http.StatusUnauthorized, gin.H{"error": "webauthn_verification_failed"})
		return
	}
	su, ok := validatedUser.(*simpleWebauthnUser)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "unexpected_user_type"})
		return
	}

	credentialID := base64.RawURLEncoding.EncodeToString(credential.ID)
	// sign_countの更新に失敗しても、ログイン自体は既に検証済みのため成功として扱う
	// (リプレイ検知の精度は落ちるが、可用性を優先する。既知の制約としてREADMEに明記)
	_ = h.WebauthnBackend.UpdateSignCount(ctx, credentialID, credential.Authenticator.SignCount)

	tokenString, exp, err := IssueLocalHMACToken(h.HMACSecret, su.userID, su.name, su.email, su.roles)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	sess := Session{
		UserID:          su.userID,
		Name:            su.name,
		Email:           su.email,
		Roles:           su.roles,
		AuthMode:        AuthModePasskey,
		AccessToken:     tokenString,
		RefreshToken:    "",
		AccessTokenExp:  exp,
		RefreshTokenExp: exp,
	}
	if err := h.establishSession(c, sess); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"authenticated": true})
}
