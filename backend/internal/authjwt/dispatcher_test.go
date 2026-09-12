package authjwt

import (
	"testing"

	"github.com/golang-jwt/jwt/v5"
)

func TestDispatcher_Verify(t *testing.T) {
	const keycloakIssuer = "http://keycloak.test/realms/training"
	const audience = "backend"
	const hmacSecret = "test-shared-secret"

	jwks := newTestJWKSServer(t)
	priv := jwks.addKey(t, "kid-1")
	keycloakVerifier := NewVerifier(jwks.server.URL, keycloakIssuer, audience)
	hmacVerifier := NewHMACVerifier(hmacSecret, LocalHMACIssuer, audience)

	dispatcher := NewDispatcher().
		Register(keycloakIssuer, keycloakVerifier).
		Register(LocalHMACIssuer, hmacVerifier)

	t.Run("Keycloak発行(iss一致)はKeycloak向けverifierに委譲される", func(t *testing.T) {
		token := mintToken(t, priv, "kid-1", baseClaims(keycloakIssuer, audience))
		claims, err := dispatcher.Verify(token)
		if err != nil {
			t.Fatalf("Verify失敗: %v", err)
		}
		if claims.Subject != "user-sub-1" {
			t.Errorf("Subject = %q, want user-sub-1", claims.Subject)
		}
	})

	t.Run("ローカルHMAC発行(iss一致)はHMAC向けverifierに委譲される", func(t *testing.T) {
		hmacToken := jwt.NewWithClaims(jwt.SigningMethodHS256, baseClaims(LocalHMACIssuer, audience))
		signed, err := hmacToken.SignedString([]byte(hmacSecret))
		if err != nil {
			t.Fatalf("署名失敗: %v", err)
		}
		claims, err := dispatcher.Verify(signed)
		if err != nil {
			t.Fatalf("Verify失敗: %v", err)
		}
		if claims.Subject != "user-sub-1" {
			t.Errorf("Subject = %q, want user-sub-1", claims.Subject)
		}
	})

	t.Run("未登録のissuerは即エラー(委譲先が無い)", func(t *testing.T) {
		token := mintToken(t, priv, "kid-1", baseClaims("http://unknown-issuer.example", audience))
		if _, err := dispatcher.Verify(token); err == nil {
			t.Fatal("エラーになるべきだが成功した")
		}
	})

	t.Run("issを詐称してもissuerが一致する委譲先の署名検証で弾かれる(2段階構造の確認)", func(t *testing.T) {
		// iss=keycloakIssuerを名乗りつつ、実際にはHMACの共有シークレットで署名したトークン
		// ParseUnverifiedで覗いたiss(keycloakIssuer)によりKeycloak向け
		// verifier(RSA/JWKS)に委譲されるが、そのverifierはRS256/JWKS公開鍵でしか
		// 検証できないため、HMAC署名は弾かれる
		forged := jwt.NewWithClaims(jwt.SigningMethodHS256, baseClaims(keycloakIssuer, audience))
		signed, err := forged.SignedString([]byte(hmacSecret))
		if err != nil {
			t.Fatalf("署名失敗: %v", err)
		}
		if _, err := dispatcher.Verify(signed); err == nil {
			t.Fatal("エラーになるべきだが成功した(iss詐称が通ってしまっている)")
		}
	})

	t.Run("壊れたトークン文字列はパース段階でエラー", func(t *testing.T) {
		if _, err := dispatcher.Verify("not-a-jwt"); err == nil {
			t.Fatal("エラーになるべきだが成功した")
		}
	})
}

func TestIsLocalIssuer(t *testing.T) {
	tests := []struct {
		iss  string
		want bool
	}{
		{LocalHMACIssuer, true},
		{LocalRSAIssuer, true},
		{"http://keycloak.test/realms/training", false},
		{"", false},
	}
	for _, tt := range tests {
		if got := IsLocalIssuer(tt.iss); got != tt.want {
			t.Errorf("IsLocalIssuer(%q) = %v, want %v", tt.iss, got, tt.want)
		}
	}
}
