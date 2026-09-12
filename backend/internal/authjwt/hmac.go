package authjwt

import (
	"fmt"

	"github.com/golang-jwt/jwt/v5"
)

// HMACVerifier はローカル(非Keycloak)認証のうちHMAC版(iss=LocalHMACIssuer)を検証する
// bff と backend が同じ共有シークレットを持つ対称鍵方式(CONTRACT.mdセクション16.4/16.5)
//
// *Verifier(JWKS/RSA版)とシグネチャを揃えてあるため、
// どちらも TokenVerifier インターフェースを満たし、Dispatcher が透過的に切り替えられる
type HMACVerifier struct {
	secret   []byte
	issuer   string
	audience string
}

func NewHMACVerifier(secret, issuer, audience string) *HMACVerifier {
	return &HMACVerifier{secret: []byte(secret), issuer: issuer, audience: audience}
}

func (v *HMACVerifier) Verify(tokenString string) (*Claims, error) {
	claims := &Claims{}
	_, err := jwt.ParseWithClaims(tokenString, claims, func(token *jwt.Token) (any, error) {
		return v.secret, nil
	},
		jwt.WithValidMethods([]string{"HS256"}),
		jwt.WithIssuer(v.issuer),
		jwt.WithAudience(v.audience),
	)
	if err != nil {
		return nil, fmt.Errorf("JWT検証失敗(HMAC): %w", err)
	}
	return claims, nil
}
