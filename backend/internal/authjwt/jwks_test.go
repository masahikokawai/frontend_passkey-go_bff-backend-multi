package authjwt

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// --- テスト用ヘルパー ---
//
// 実際のKeycloakを使わず、テスト内でRSA鍵ペアを生成し、フェイクのJWKSエンドポイントを httptest.Server で立てる
// これによりiss/aud/exp/alg/kidローテーションの各分岐を
// ネットワークやKeycloakの起動なしに検証できる

type testJWKSServer struct {
	server   *httptest.Server
	requests int32
	keys     map[string]*rsa.PrivateKey
}

func newTestJWKSServer(t *testing.T) *testJWKSServer {
	t.Helper()
	s := &testJWKSServer{keys: map[string]*rsa.PrivateKey{}}
	s.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&s.requests, 1)
		type jwk struct {
			Kty string `json:"kty"`
			Kid string `json:"kid"`
			Use string `json:"use"`
			N   string `json:"n"`
			E   string `json:"e"`
		}
		keys := make([]jwk, 0, len(s.keys))
		for kid, priv := range s.keys {
			keys = append(keys, jwk{
				Kty: "RSA",
				Kid: kid,
				Use: "sig",
				N:   base64.RawURLEncoding.EncodeToString(priv.PublicKey.N.Bytes()),
				E:   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(priv.PublicKey.E)).Bytes()),
			})
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"keys": keys})
	}))
	t.Cleanup(s.server.Close)
	return s
}

// addKey はJWKSサーバーが公開する鍵を1つ追加し、対応する秘密鍵を返す
// Keycloakの鍵ローテーション(新しいkidが増える)を模擬するために使う
func (s *testJWKSServer) addKey(t *testing.T, kid string) *rsa.PrivateKey {
	t.Helper()
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("RSA鍵生成に失敗: %v", err)
	}
	s.keys[kid] = priv
	return priv
}

func (s *testJWKSServer) requestCount() int32 {
	return atomic.LoadInt32(&s.requests)
}

// mintToken はRS256で署名したJWTを生成する
func mintToken(t *testing.T, priv *rsa.PrivateKey, kid string, claims jwt.MapClaims) string {
	t.Helper()
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["kid"] = kid
	signed, err := token.SignedString(priv)
	if err != nil {
		t.Fatalf("トークン署名に失敗: %v", err)
	}
	return signed
}

// mintHS256Token はRS256以外の署名アルゴリズムを検証エラーとして弾くことを
// 確認するために、意図的にHS256で署名したJWTを生成する
func mintHS256Token(t *testing.T, claims jwt.MapClaims) string {
	t.Helper()
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString([]byte("dummy-secret-not-used-for-rs256"))
	if err != nil {
		t.Fatalf("トークン署名に失敗: %v", err)
	}
	return signed
}

// baseClaims はiss/exp/iat/sub入りの最小クレームを組み立てる
// audは空文字だと省略する(実機で発生した「audクレーム自体が無い」ケースの再現のため)
func baseClaims(iss, aud string) jwt.MapClaims {
	now := time.Now()
	c := jwt.MapClaims{
		"iss": iss,
		"exp": now.Add(time.Hour).Unix(),
		"iat": now.Unix(),
		"sub": "user-sub-1",
	}
	if aud != "" {
		c["aud"] = aud
	}
	return c
}

func TestVerifier_Verify(t *testing.T) {
	jwks := newTestJWKSServer(t)
	priv := jwks.addKey(t, "kid-1")
	const issuer = "http://keycloak.test/realms/training"
	const audience = "backend"

	verifier := NewVerifier(jwks.server.URL, issuer, audience)

	t.Run("正しいiss/aud/alg/期限内なら成功しClaimsが取れる", func(t *testing.T) {
		claims := baseClaims(issuer, audience)
		claims["preferred_username"] = "taro"
		token := mintToken(t, priv, "kid-1", claims)

		got, err := verifier.Verify(token)
		if err != nil {
			t.Fatalf("Verify失敗: %v", err)
		}
		if got.Subject != "user-sub-1" {
			t.Errorf("Subject = %q, want %q", got.Subject, "user-sub-1")
		}
		if got.PreferredUsername != "taro" {
			t.Errorf("PreferredUsername = %q, want %q", got.PreferredUsername, "taro")
		}
	})

	t.Run("issが異なるとエラー", func(t *testing.T) {
		claims := baseClaims("http://other-issuer.example", audience)
		token := mintToken(t, priv, "kid-1", claims)
		if _, err := verifier.Verify(token); err == nil {
			t.Fatal("エラーになるべきだが成功した")
		}
	})

	t.Run("audが異なるとエラー", func(t *testing.T) {
		claims := baseClaims(issuer, "other-audience")
		token := mintToken(t, priv, "kid-1", claims)
		if _, err := verifier.Verify(token); err == nil {
			t.Fatal("エラーになるべきだが成功した")
		}
	})

	t.Run("audクレーム自体が無いとエラー(実機で発生した不具合と同じケース)", func(t *testing.T) {
		// Keycloakの認可コードフローで発行されるAccess Tokenにはaudクレームが
		// 一切含まれないケースが実際にあり、これが本番相当環境で発覚した不具合だった
		claims := baseClaims(issuer, "")
		token := mintToken(t, priv, "kid-1", claims)
		if _, err := verifier.Verify(token); err == nil {
			t.Fatal("エラーになるべきだが成功した")
		}
	})

	t.Run("有効期限切れはエラー", func(t *testing.T) {
		claims := baseClaims(issuer, audience)
		claims["exp"] = time.Now().Add(-time.Hour).Unix()
		token := mintToken(t, priv, "kid-1", claims)
		if _, err := verifier.Verify(token); err == nil {
			t.Fatal("エラーになるべきだが成功した")
		}
	})

	t.Run("RS256以外の署名アルゴリズムはエラー", func(t *testing.T) {
		claims := baseClaims(issuer, audience)
		token := mintHS256Token(t, claims)
		if _, err := verifier.Verify(token); err == nil {
			t.Fatal("エラーになるべきだが成功した")
		}
	})

	t.Run("未知のkidの場合はJWKSを再取得する", func(t *testing.T) {
		before := jwks.requestCount()
		// まだVerifierにキャッシュされていない新しい鍵をJWKSサーバー側に追加する
		// (Keycloakの鍵ローテーションを模擬)
		// この鍵で署名したトークンを検証すると、keyFunc がキャッシュミス → JWKS 再取得を行うはずである
		newPriv := jwks.addKey(t, "kid-2")
		claims := baseClaims(issuer, audience)
		token := mintToken(t, newPriv, "kid-2", claims)

		if _, err := verifier.Verify(token); err != nil {
			t.Fatalf("Verify失敗: %v", err)
		}
		after := jwks.requestCount()
		if after <= before {
			t.Errorf("JWKSの再取得が発生していない(before=%d after=%d)", before, after)
		}
	})

	t.Run("nbf(未来日時)はエラー", func(t *testing.T) {
		// exp(期限切れ)側は既にテストがあったが、対になるnbf(まだ有効になっていない)側が未検証だった
		//
		// golang-jwt/v5はParseWithClaimsがexp/nbf/iatを既定で
		// 自動検証するため実装コードの追加は不要なはずだが、「本当にnbfを見ているか」は
		// このテストが無いと保証できない(検証ライブラリの挙動変更や設定変更の
		// リグレッションを検知するためのテスト)
		claims := baseClaims(issuer, audience)
		claims["nbf"] = time.Now().Add(time.Hour).Unix()
		token := mintToken(t, priv, "kid-1", claims)
		if _, err := verifier.Verify(token); err == nil {
			t.Fatal("エラーになるべきだが成功した")
		}
	})

	t.Run("alg=noneの署名無しトークンはエラー(JWTの古典的な脆弱性パターンの否定テスト)", func(t *testing.T) {
		// 「alg: none」を名乗り署名検証自体をスキップさせようとする、JWT 実装の定番の攻撃パターン
		// WithValidMethods([]string{"RS256"}) により弾かれる
		// はずだが、実際にこの攻撃パターンそのものを再現したテストが無かった
		claims := baseClaims(issuer, audience)
		token := jwt.NewWithClaims(jwt.SigningMethodNone, claims)
		signed, err := token.SignedString(jwt.UnsafeAllowNoneSignatureType)
		if err != nil {
			t.Fatalf("alg=noneトークンの生成に失敗: %v", err)
		}
		if _, err := verifier.Verify(signed); err == nil {
			t.Fatal("エラーになるべきだが成功した(alg=noneが受理されてしまっている)")
		}
	})

	t.Run("JWKSに存在しないkidを騙るトークンは再取得後もエラーになる", func(t *testing.T) {
		// ローカルRSA認証(CONTRACT.mdセクション16.4)のkidタンパリング(存在しないkidへの書き換え)を想定した否定テスト
		//
		// 上の「未知のkidの場合は再取得する」は
		// 再取得後に見つかる正常系のみだったため、そもそもJWKS側に存在しないkidを
		// 名乗った場合に最終的にエラーで弾かれることまでは確認できていなかった
		// (priv自体は正規のkid-1の鍵だが、ヘッダのkidだけ存在しない値に差し替える)
		token := mintToken(t, priv, "kid-does-not-exist", baseClaims(issuer, audience))
		if _, err := verifier.Verify(token); err == nil {
			t.Fatal("エラーになるべきだが成功した")
		}
	})
}

func TestVerifier_Prefetch(t *testing.T) {
	jwks := newTestJWKSServer(t)
	jwks.addKey(t, "kid-1")
	verifier := NewVerifier(jwks.server.URL, "iss", "aud")
	if err := verifier.Prefetch(); err != nil {
		t.Fatalf("Prefetch失敗: %v", err)
	}
	if jwks.requestCount() == 0 {
		t.Error("Prefetchでリクエストが発生していない")
	}
}
