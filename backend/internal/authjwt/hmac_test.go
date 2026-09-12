package authjwt

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestHMACVerifier_Verify(t *testing.T) {
	const issuer = LocalHMACIssuer
	const audience = "backend"
	const secret = "test-shared-secret"
	verifier := NewHMACVerifier(secret, issuer, audience)

	mint := func(claims jwt.MapClaims, signingSecret string) string {
		token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
		signed, err := token.SignedString([]byte(signingSecret))
		if err != nil {
			t.Fatalf("トークン署名に失敗: %v", err)
		}
		return signed
	}

	t.Run("正しいシークレット/iss/aud/期限内なら成功しClaimsが取れる", func(t *testing.T) {
		token := mint(baseClaims(issuer, audience), secret)
		claims, err := verifier.Verify(token)
		if err != nil {
			t.Fatalf("Verify失敗: %v", err)
		}
		if claims.Subject != "user-sub-1" {
			t.Errorf("Subject = %q, want user-sub-1", claims.Subject)
		}
	})

	t.Run("シークレットが違うとエラー", func(t *testing.T) {
		token := mint(baseClaims(issuer, audience), "wrong-secret")
		if _, err := verifier.Verify(token); err == nil {
			t.Fatal("エラーになるべきだが成功した")
		}
	})

	t.Run("issが違うとエラー", func(t *testing.T) {
		token := mint(baseClaims("other-issuer", audience), secret)
		if _, err := verifier.Verify(token); err == nil {
			t.Fatal("エラーになるべきだが成功した")
		}
	})

	t.Run("audが違うとエラー", func(t *testing.T) {
		token := mint(baseClaims(issuer, "other-audience"), secret)
		if _, err := verifier.Verify(token); err == nil {
			t.Fatal("エラーになるべきだが成功した")
		}
	})

	t.Run("有効期限切れはエラー", func(t *testing.T) {
		claims := baseClaims(issuer, audience)
		claims["exp"] = time.Now().Add(-time.Hour).Unix()
		token := mint(claims, secret)
		if _, err := verifier.Verify(token); err == nil {
			t.Fatal("エラーになるべきだが成功した")
		}
	})

	t.Run("nbf(未来日時)はエラー", func(t *testing.T) {
		// jwks_test.goのTestVerifier_Verifyに対になるテストを追加した理由と同じ
		// (exp側は既存、nbf側が未検証だった)
		claims := baseClaims(issuer, audience)
		claims["nbf"] = time.Now().Add(time.Hour).Unix()
		token := mint(claims, secret)
		if _, err := verifier.Verify(token); err == nil {
			t.Fatal("エラーになるべきだが成功した")
		}
	})

	t.Run("alg=noneの署名無しトークンはエラー", func(t *testing.T) {
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

	t.Run("RS256で署名されたトークンはHS256専用のこのVerifierでは弾かれる", func(t *testing.T) {
		// アルゴリズム混同攻撃対策の確認
		// HMACVerifier は ValidMethods=HS256 を要求するため、RS256で署名されたトークンはアルゴリズム検証段階で弾かれる
		jwks := newTestJWKSServer(t)
		priv := jwks.addKey(t, "kid-1")
		token := mintToken(t, priv, "kid-1", baseClaims(issuer, audience))
		if _, err := verifier.Verify(token); err == nil {
			t.Fatal("エラーになるべきだが成功した")
		}
	})
}
