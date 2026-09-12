package auth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/descope/virtualwebauthn"
	"github.com/gin-gonic/gin"
)

// このファイルはCONTRACT.mdセクション22のパスキー登録・ログインを、実際に有効な
// WebAuthn attestation/assertion(本物の署名検証を通る暗号データ)で end-to-end 検証する
//
// 【テスト監査で追記した理由】
// 既存のwebauthn_test.goはガード条件(未ログイン・スコープ外・
// challenge不在等)は網羅していたが、go-webauthnライブラリが実際に行う署名検証まで
// 通す「本当に登録・ログインが成功するケース」が1件も無かった
//
// この経路はパスキー機能の心臓部であり、ここが壊れていてもガード条件のテストは全てpassし続けてしまう
// (静かな回帰に最も弱い箇所)
// `github.com/descope/virtualwebauthn`
// (仮想認証器としてWeb Authentication APIの挙動をテストコード内から再現するライブラリ)を
// 使い、ブラウザ・実機の認証器を使わずに本物の暗号データでこの経路を検証する
func TestWebauthnRegisterAndLogin_実際のattestation_assertionで一連の流れが成功する(t *testing.T) {
	gin.SetMode(gin.TestMode)

	backend := &fakeWebauthnBackend{}
	h, store := newTestWebauthnHandler(t, backend)
	sessionID := createTestSessionWithMode(t, store, AuthModeLocalHMAC)

	rp := virtualwebauthn.RelyingParty{ID: "localhost", Name: "bff-gin test", Origin: "http://localhost:5173"}
	// 【デバッグで判明した必須設定】discoverable credential(usernameless)方式では、
	// go-webauthnのFinishPasskeyLoginがassertionレスポンスの userHandle を必須で要求する
	// (空だと"Client-side Discoverable Assertion was attempted with a blank User Handle"で拒否される)
	// userHandleは`simpleWebauthnUser.WebAuthnID()`が返す値
	// (strconv.FormatUint(userID, 10))と一致させる必要がある
	// createTestSessionWithMode が UserID: 1 のセッションを作るため、ここも"1"に合わせる
	authenticator := virtualwebauthn.NewAuthenticatorWithOptions(virtualwebauthn.AuthenticatorOptions{
		UserHandle: []byte("1"),
	})
	cred := virtualwebauthn.NewCredential(virtualwebauthn.KeyTypeEC2)
	authenticator.AddCredential(cred)

	// --- 登録(register/begin → register/finish) ---
	router := gin.New()
	router.Use(RequireSession(store, testCookieConfig()))
	router.POST("/api/auth/passkey/register/begin", h.WebauthnRegisterBegin)
	router.POST("/api/auth/passkey/register/finish", h.WebauthnRegisterFinish)

	beginReq := httptest.NewRequest(http.MethodPost, "/api/auth/passkey/register/begin", nil)
	beginReq.AddCookie(&http.Cookie{Name: testCookieConfig().SessionCookieName, Value: sessionID})
	beginRec := httptest.NewRecorder()
	router.ServeHTTP(beginRec, beginReq)
	if beginRec.Code != http.StatusOK {
		t.Fatalf("register/begin status = %d, body=%s", beginRec.Code, beginRec.Body.String())
	}
	var creation struct {
		PublicKey struct {
			Challenge string `json:"challenge"`
			RP        struct {
				ID string `json:"id"`
			} `json:"rp"`
		} `json:"publicKey"`
	}
	if err := json.Unmarshal(beginRec.Body.Bytes(), &creation); err != nil {
		t.Fatalf("register/begin レスポンスのパース失敗: %v", err)
	}
	challengeBytes, err := base64.RawURLEncoding.DecodeString(creation.PublicKey.Challenge)
	if err != nil {
		t.Fatalf("challengeのbase64urlデコード失敗: %v", err)
	}

	attestationJSON := virtualwebauthn.CreateAttestationResponse(rp, authenticator, cred, virtualwebauthn.AttestationOptions{
		Challenge:      challengeBytes,
		RelyingPartyID: creation.PublicKey.RP.ID,
	})

	finishReq := httptest.NewRequest(http.MethodPost, "/api/auth/passkey/register/finish?name=テスト用MacBook", strings.NewReader(attestationJSON))
	finishReq.Header.Set("Content-Type", "application/json")
	finishReq.AddCookie(&http.Cookie{Name: testCookieConfig().SessionCookieName, Value: sessionID})
	finishRec := httptest.NewRecorder()
	router.ServeHTTP(finishRec, finishReq)
	if finishRec.Code != http.StatusCreated {
		t.Fatalf("register/finish status = %d, want 201, body=%s", finishRec.Code, finishRec.Body.String())
	}
	if backend.registeredCredentialID == "" || backend.registeredPublicKey == "" {
		t.Fatalf("backendへの登録呼び出しが記録されていない(credential_id/public_keyが空)")
	}
	if backend.registeredName != "テスト用MacBook" {
		t.Errorf("登録時のname = %q, want テスト用MacBook(?name=クエリの値が伝わっていること)", backend.registeredName)
	}

	// --- ログイン(login/begin → login/finish) ---
	// 実際のbackendには登録していないため、直前の登録で記録した公開鍵をそのまま
	// fakeWebauthnBackend.Lookupの戻り値に設定し、「backend照会結果」を模倣する
	//
	// 【デバッグで判明】
	// lookupUserIDは登録時に使ったセッションの UserID(=1、createTestSessionWithMode参照)と必ず一致させる必要がある
	// assertion の userHandle(認証器側でWebAuthnID()="1"として焼き込まれている)と、
	// Lookup が返す user_id から go-webauthn が組み立てる WebAuthnID() が食い違うと、
	// go-webauthn内部の userHandle 突き合わせで検証エラーになる
	// (実運用では同一credentialの登録時/ログイン時のuser_idは常に一致するため
	// 起きない不整合だが、テストで別のuser_idを使うと再現してしまう)
	backend.lookupUserID = 1
	backend.lookupName = "ローカル太郎"
	backend.lookupEmail = "local-user@example.com"
	backend.lookupRoles = []string{"general"}
	backend.lookupPublicKey = backend.registeredPublicKey
	backend.lookupSignCount = 0
	// 【実機デバッグで追記、回帰テスト化】登録時に記録したBE/BSフラグをログイン時の
	// Lookupでも使い回さないと、go-webauthnが「登録時と申告内容が矛盾している」として
	// 正当なログインを拒否する("Backup Eligible flag inconsistency detected")。
	// 実機のバグはこのフラグが常にfalseのまま保存されていたことが原因だったので、
	// registeredBackupEligible/Stateをそのまま使い回す(=一致させる)のが正しい再現
	backend.lookupBackupEligible = backend.registeredBackupEligible
	backend.lookupBackupState = backend.registeredBackupState

	loginRouter := gin.New()
	loginRouter.POST("/api/auth/passkey/login/begin", h.WebauthnLoginBegin)
	loginRouter.POST("/api/auth/passkey/login/finish", h.WebauthnLoginFinish)

	loginBeginRec := httptest.NewRecorder()
	loginRouter.ServeHTTP(loginBeginRec, httptest.NewRequest(http.MethodPost, "/api/auth/passkey/login/begin", nil))
	if loginBeginRec.Code != http.StatusOK {
		t.Fatalf("login/begin status = %d, body=%s", loginBeginRec.Code, loginBeginRec.Body.String())
	}
	var loginBegin struct {
		PublicKey struct {
			Challenge string `json:"challenge"`
			RPID      string `json:"rpId"`
		} `json:"publicKey"`
		State string `json:"state"`
	}
	if err := json.Unmarshal(loginBeginRec.Body.Bytes(), &loginBegin); err != nil {
		t.Fatalf("login/begin レスポンスのパース失敗: %v", err)
	}
	loginChallengeBytes, err := base64.RawURLEncoding.DecodeString(loginBegin.PublicKey.Challenge)
	if err != nil {
		t.Fatalf("login challengeのbase64urlデコード失敗: %v", err)
	}

	assertionJSON := virtualwebauthn.CreateAssertionResponse(rp, authenticator, cred, virtualwebauthn.AssertionOptions{
		Challenge:      loginChallengeBytes,
		RelyingPartyID: loginBegin.PublicKey.RPID,
	})

	loginFinishReq := httptest.NewRequest(http.MethodPost, "/api/auth/passkey/login/finish?state="+loginBegin.State, strings.NewReader(assertionJSON))
	loginFinishReq.Header.Set("Content-Type", "application/json")
	loginFinishRec := httptest.NewRecorder()
	loginRouter.ServeHTTP(loginFinishRec, loginFinishReq)
	if loginFinishRec.Code != http.StatusOK {
		t.Fatalf("login/finish status = %d, want 200, body=%s", loginFinishRec.Code, loginFinishRec.Body.String())
	}

	// セッションが実際に発行され、auth_mode=passkeyかつ自前署名のHMACトークンが
	// 入っていること(CONTRACT.mdセクション22の設計通り、backend呼び出し用の
	// access_tokenがKeycloak/ローカルパスワード認証と同じ経路で用意されること)を確認する
	var setCookie string
	for _, c := range loginFinishRec.Result().Cookies() {
		if c.Name == testCookieConfig().SessionCookieName {
			setCookie = c.Value
		}
	}
	if setCookie == "" {
		t.Fatal("login/finish がセッションCookieを発行していない")
	}
	newSession, err := store.Get(context.Background(), setCookie)
	if err != nil {
		t.Fatalf("発行されたセッションをRedisから取得できない: %v", err)
	}
	if newSession.AuthMode != AuthModePasskey {
		t.Errorf("AuthMode = %q, want %q", newSession.AuthMode, AuthModePasskey)
	}
	if newSession.UserID != 1 {
		t.Errorf("UserID = %d, want 1", newSession.UserID)
	}
	if newSession.AccessToken == "" {
		t.Error("AccessTokenが空(backend呼び出し用の自前署名JWTが発行されていない)")
	}
	if backend.updateSignCountCalled != true {
		t.Error("ログイン成功後にUpdateSignCountが呼ばれていない(リプレイ対策のsign_count更新)")
	}
}

// 【テスト監査で追加(2026-09-12)】CONTRACT.mdセクション22.8の実機バグの再発防止テスト
//
// 上のTestWebauthnRegisterAndLogin_...は、登録時と同じBE/BS値をLookupスタブへそのまま
// 使い回す「一致するケース」しか検証しておらず、go-webauthnが実際にBE/BS不一致を
// 検知して拒否することそのものは一度も検証されていなかった(値の配線経路が正しいことしか
// 証明できておらず、go-webauthnの検証ロジックがその配線を実際に活用しているかは未確認だった)
//
// virtualwebauthn(仮想認証器)は既定でBackupEligible=falseを申告するアサーションしか
// 生成できないため、Lookupスタブ側にわざと矛盾する値(true)を入れることで
// 「backendに保存されている値」と「認証器が実際に申告する値」の不一致を人為的に再現し、
// go-webauthnがこれを検知してログインを拒否することを確認する
// (実機バグは逆方向 - 常にfalseで保存 - だったが、検知ロジック自体はどちら向きの不一致でも
// 対称に働くはずで、このテストはその検知ロジックそのものが機能していることの証明になる)
func TestWebauthnLoginFinish_登録時と異なるBackupEligibleを申告するとログインが拒否される(t *testing.T) {
	gin.SetMode(gin.TestMode)

	backend := &fakeWebauthnBackend{}
	h, store := newTestWebauthnHandler(t, backend)
	sessionID := createTestSessionWithMode(t, store, AuthModeLocalHMAC)

	rp := virtualwebauthn.RelyingParty{ID: "localhost", Name: "bff-gin test", Origin: "http://localhost:5173"}
	authenticator := virtualwebauthn.NewAuthenticatorWithOptions(virtualwebauthn.AuthenticatorOptions{
		UserHandle: []byte("1"),
	})
	cred := virtualwebauthn.NewCredential(virtualwebauthn.KeyTypeEC2)
	authenticator.AddCredential(cred)

	router := gin.New()
	router.Use(RequireSession(store, testCookieConfig()))
	router.POST("/api/auth/passkey/register/begin", h.WebauthnRegisterBegin)
	router.POST("/api/auth/passkey/register/finish", h.WebauthnRegisterFinish)

	beginReq := httptest.NewRequest(http.MethodPost, "/api/auth/passkey/register/begin", nil)
	beginReq.AddCookie(&http.Cookie{Name: testCookieConfig().SessionCookieName, Value: sessionID})
	beginRec := httptest.NewRecorder()
	router.ServeHTTP(beginRec, beginReq)
	if beginRec.Code != http.StatusOK {
		t.Fatalf("register/begin status = %d, body=%s", beginRec.Code, beginRec.Body.String())
	}
	var creation struct {
		PublicKey struct {
			Challenge string `json:"challenge"`
			RP        struct {
				ID string `json:"id"`
			} `json:"rp"`
		} `json:"publicKey"`
	}
	if err := json.Unmarshal(beginRec.Body.Bytes(), &creation); err != nil {
		t.Fatalf("register/begin レスポンスのパース失敗: %v", err)
	}
	challengeBytes, err := base64.RawURLEncoding.DecodeString(creation.PublicKey.Challenge)
	if err != nil {
		t.Fatalf("challengeのbase64urlデコード失敗: %v", err)
	}
	attestationJSON := virtualwebauthn.CreateAttestationResponse(rp, authenticator, cred, virtualwebauthn.AttestationOptions{
		Challenge:      challengeBytes,
		RelyingPartyID: creation.PublicKey.RP.ID,
	})
	finishReq := httptest.NewRequest(http.MethodPost, "/api/auth/passkey/register/finish", strings.NewReader(attestationJSON))
	finishReq.Header.Set("Content-Type", "application/json")
	finishReq.AddCookie(&http.Cookie{Name: testCookieConfig().SessionCookieName, Value: sessionID})
	finishRec := httptest.NewRecorder()
	router.ServeHTTP(finishRec, finishReq)
	if finishRec.Code != http.StatusCreated {
		t.Fatalf("register/finish status = %d, want 201, body=%s", finishRec.Code, finishRec.Body.String())
	}

	backend.lookupUserID = 1
	backend.lookupName = "ローカル太郎"
	backend.lookupEmail = "local-user@example.com"
	backend.lookupRoles = []string{"general"}
	backend.lookupPublicKey = backend.registeredPublicKey
	backend.lookupSignCount = 0
	// 【本題】virtualwebauthnは常にBackupEligible=falseを申告するアサーションを生成するため、
	// Lookupがtrueを返すよう意図的に食い違わせる(実機バグの逆方向の不一致を人為的に再現)
	backend.lookupBackupEligible = true
	backend.lookupBackupState = false

	loginRouter := gin.New()
	loginRouter.POST("/api/auth/passkey/login/begin", h.WebauthnLoginBegin)
	loginRouter.POST("/api/auth/passkey/login/finish", h.WebauthnLoginFinish)

	loginBeginRec := httptest.NewRecorder()
	loginRouter.ServeHTTP(loginBeginRec, httptest.NewRequest(http.MethodPost, "/api/auth/passkey/login/begin", nil))
	if loginBeginRec.Code != http.StatusOK {
		t.Fatalf("login/begin status = %d, body=%s", loginBeginRec.Code, loginBeginRec.Body.String())
	}
	var loginBegin struct {
		PublicKey struct {
			Challenge string `json:"challenge"`
			RPID      string `json:"rpId"`
		} `json:"publicKey"`
		State string `json:"state"`
	}
	if err := json.Unmarshal(loginBeginRec.Body.Bytes(), &loginBegin); err != nil {
		t.Fatalf("login/begin レスポンスのパース失敗: %v", err)
	}
	loginChallengeBytes, err := base64.RawURLEncoding.DecodeString(loginBegin.PublicKey.Challenge)
	if err != nil {
		t.Fatalf("login challengeのbase64urlデコード失敗: %v", err)
	}
	assertionJSON := virtualwebauthn.CreateAssertionResponse(rp, authenticator, cred, virtualwebauthn.AssertionOptions{
		Challenge:      loginChallengeBytes,
		RelyingPartyID: loginBegin.PublicKey.RPID,
	})

	loginFinishReq := httptest.NewRequest(http.MethodPost, "/api/auth/passkey/login/finish?state="+loginBegin.State, strings.NewReader(assertionJSON))
	loginFinishReq.Header.Set("Content-Type", "application/json")
	loginFinishRec := httptest.NewRecorder()
	loginRouter.ServeHTTP(loginFinishRec, loginFinishReq)

	// go-webauthnが「登録時に保存されたBE/BSと、実際の申告内容が矛盾している」として
	// 拒否することを期待する(200では絶対にないこと、これが本テストの核心)
	if loginFinishRec.Code == http.StatusOK {
		t.Fatalf("BE不一致にもかかわらずログインが成功してしまった(status=200)。"+
			"go-webauthnのBE/BS一貫性チェックが機能していない、またはbff側の配線が壊れている可能性がある: body=%s",
			loginFinishRec.Body.String())
	}
	if backend.updateSignCountCalled {
		t.Error("ログイン拒否されたにもかかわらずUpdateSignCountが呼ばれている(検証失敗後に副作用が実行されている)")
	}
}

// 【テスト監査で追加(2026-09-12)】並行性の観点の監査で判明: backendのUpdateSignCount実装
// (internal/repository/webauthn_credential.go)は"UPDATE ... SET sign_count = ?"という
// 素朴な(compare-and-swapではない)更新であり、同じ古いsign_countを起点に2つのログインが
// 並行実行されると、両方が検証を通過しうる(パスキーchallengeで既に見つかったのと同じ
// クラスのTOCTOU)。ただしbff側のWebauthnLoginFinishは意図的にUpdateSignCountの
// エラーを握りつぶし(webauthn.go: "sign_countの更新に失敗しても、ログイン自体は
// 既に検証済みのため成功として扱う...可用性を優先する")、ログイン自体は失敗させない
// 設計になっている。つまりbackend側をcompare-and-swapに直しても、bffがそのエラーを
// 無視する限りログインの可否には影響しない(可用性優先というこの設計判断自体を
// 変えない限り、replay検知の強化は意味を持たない)。
//
// この既存の(意図的な)挙動を固定するための回帰テスト: UpdateSignCountが失敗しても、
// ログイン自体は成功しセッションが発行されることを確認する。将来ここが暗黙に
// 変わっていないかを検知する目的(CONTRACT.mdセクション23.2に既知の制約として追記済み)
func TestWebauthnLoginFinish_UpdateSignCountが失敗してもログイン自体は成功する(t *testing.T) {
	gin.SetMode(gin.TestMode)

	backend := &fakeWebauthnBackend{}
	h, store := newTestWebauthnHandler(t, backend)
	sessionID := createTestSessionWithMode(t, store, AuthModeLocalHMAC)

	rp := virtualwebauthn.RelyingParty{ID: "localhost", Name: "bff-gin test", Origin: "http://localhost:5173"}
	authenticator := virtualwebauthn.NewAuthenticatorWithOptions(virtualwebauthn.AuthenticatorOptions{
		UserHandle: []byte("1"),
	})
	cred := virtualwebauthn.NewCredential(virtualwebauthn.KeyTypeEC2)
	authenticator.AddCredential(cred)

	router := gin.New()
	router.Use(RequireSession(store, testCookieConfig()))
	router.POST("/api/auth/passkey/register/begin", h.WebauthnRegisterBegin)
	router.POST("/api/auth/passkey/register/finish", h.WebauthnRegisterFinish)

	beginReq := httptest.NewRequest(http.MethodPost, "/api/auth/passkey/register/begin", nil)
	beginReq.AddCookie(&http.Cookie{Name: testCookieConfig().SessionCookieName, Value: sessionID})
	beginRec := httptest.NewRecorder()
	router.ServeHTTP(beginRec, beginReq)
	if beginRec.Code != http.StatusOK {
		t.Fatalf("register/begin status = %d, body=%s", beginRec.Code, beginRec.Body.String())
	}
	var creation struct {
		PublicKey struct {
			Challenge string `json:"challenge"`
			RP        struct {
				ID string `json:"id"`
			} `json:"rp"`
		} `json:"publicKey"`
	}
	if err := json.Unmarshal(beginRec.Body.Bytes(), &creation); err != nil {
		t.Fatalf("register/begin レスポンスのパース失敗: %v", err)
	}
	challengeBytes, err := base64.RawURLEncoding.DecodeString(creation.PublicKey.Challenge)
	if err != nil {
		t.Fatalf("challengeのbase64urlデコード失敗: %v", err)
	}
	attestationJSON := virtualwebauthn.CreateAttestationResponse(rp, authenticator, cred, virtualwebauthn.AttestationOptions{
		Challenge:      challengeBytes,
		RelyingPartyID: creation.PublicKey.RP.ID,
	})
	finishReq := httptest.NewRequest(http.MethodPost, "/api/auth/passkey/register/finish", strings.NewReader(attestationJSON))
	finishReq.Header.Set("Content-Type", "application/json")
	finishReq.AddCookie(&http.Cookie{Name: testCookieConfig().SessionCookieName, Value: sessionID})
	finishRec := httptest.NewRecorder()
	router.ServeHTTP(finishRec, finishReq)
	if finishRec.Code != http.StatusCreated {
		t.Fatalf("register/finish status = %d, want 201, body=%s", finishRec.Code, finishRec.Body.String())
	}

	backend.lookupUserID = 1
	backend.lookupName = "ローカル太郎"
	backend.lookupEmail = "local-user@example.com"
	backend.lookupRoles = []string{"general"}
	backend.lookupPublicKey = backend.registeredPublicKey
	backend.lookupSignCount = 0
	backend.lookupBackupEligible = backend.registeredBackupEligible
	backend.lookupBackupState = backend.registeredBackupState
	// 【本題】sign_countの更新がbackend側で失敗する状況を模す
	// (例: 並行リクエストによる更新競合、DB障害等)
	backend.updateSignCountErr = errors.New("db conflict (simulated)")

	loginRouter := gin.New()
	loginRouter.POST("/api/auth/passkey/login/begin", h.WebauthnLoginBegin)
	loginRouter.POST("/api/auth/passkey/login/finish", h.WebauthnLoginFinish)

	loginBeginRec := httptest.NewRecorder()
	loginRouter.ServeHTTP(loginBeginRec, httptest.NewRequest(http.MethodPost, "/api/auth/passkey/login/begin", nil))
	if loginBeginRec.Code != http.StatusOK {
		t.Fatalf("login/begin status = %d, body=%s", loginBeginRec.Code, loginBeginRec.Body.String())
	}
	var loginBegin struct {
		PublicKey struct {
			Challenge string `json:"challenge"`
			RPID      string `json:"rpId"`
		} `json:"publicKey"`
		State string `json:"state"`
	}
	if err := json.Unmarshal(loginBeginRec.Body.Bytes(), &loginBegin); err != nil {
		t.Fatalf("login/begin レスポンスのパース失敗: %v", err)
	}
	loginChallengeBytes, err := base64.RawURLEncoding.DecodeString(loginBegin.PublicKey.Challenge)
	if err != nil {
		t.Fatalf("login challengeのbase64urlデコード失敗: %v", err)
	}
	assertionJSON := virtualwebauthn.CreateAssertionResponse(rp, authenticator, cred, virtualwebauthn.AssertionOptions{
		Challenge:      loginChallengeBytes,
		RelyingPartyID: loginBegin.PublicKey.RPID,
	})

	loginFinishReq := httptest.NewRequest(http.MethodPost, "/api/auth/passkey/login/finish?state="+loginBegin.State, strings.NewReader(assertionJSON))
	loginFinishReq.Header.Set("Content-Type", "application/json")
	loginFinishRec := httptest.NewRecorder()
	loginRouter.ServeHTTP(loginFinishRec, loginFinishReq)

	// 【本題】UpdateSignCountがエラーを返しても、ログイン自体は200で成功する
	// (可用性優先の設計、この挙動が暗黙に変わっていないことを固定する)
	if loginFinishRec.Code != http.StatusOK {
		t.Fatalf("login/finish status = %d, want 200(UpdateSignCount失敗時もログインは成功する設計のはず), body=%s",
			loginFinishRec.Code, loginFinishRec.Body.String())
	}
	if !backend.updateSignCountCalled {
		t.Error("UpdateSignCountが呼ばれていない")
	}
	var setCookie string
	for _, c := range loginFinishRec.Result().Cookies() {
		if c.Name == testCookieConfig().SessionCookieName {
			setCookie = c.Value
		}
	}
	if setCookie == "" {
		t.Fatal("UpdateSignCount失敗時もセッションCookieが発行されるはずだが、発行されていない")
	}
}
