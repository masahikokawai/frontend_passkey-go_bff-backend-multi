// ローカル(非Keycloak)認証で bff 自身が JWT を発行する処理(CONTRACT.mdセクション16.4)
//
// Keycloak ログインでは bff は「JWTを検証するだけ」(oidc.go)だったが、ローカル認証では bff 自身が Issuer(発行者)になる
// HMAC(HS256、共有シークレット)とRSA(RS256、bff が自分の鍵ペアを持ち JWKS を公開する)の2方式を比較用に両方実装する
package auth

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"fmt"
	"strconv"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const (
	// LocalHMACIssuer / LocalRSAIssuer はbffが発行するローカルJWTの `iss` クレーム
	// backend側の authjwt.Dispatcher がこの値でどのverifierを使うか振り分ける
	// (CONTRACT.mdセクション16.5)
	LocalHMACIssuer = "bff-gin-local-hmac"
	LocalRSAIssuer  = "bff-gin-local-rsa"

	// localTokenAudience はbackendのExpectedAudienceと一致させる
	localTokenAudience = "backend"
	// localTokenTTL はローカルセッションのJWT有効期限
	// refresh_token が無いためこの期限が切れたら再ログインが必要になる(CONTRACT.mdセクション16.4)
	localTokenTTL = 24 * time.Hour

	// localRSAKeyID は bff が発行する RSA 署名 JWT の kid
	// 鍵は1本しか持たない
	// (プロセス起動時に1回生成するだけで、鍵ローテーションは行わない学習用の簡略化)ため固定値で良い
	localRSAKeyID = "bff-local-rsa-1"
)

// LocalClaims はbffが自分で発行するJWTのペイロード
// backend側の authjwt.Claims と互換の形(RegisteredClaims埋め込み+role相当のroles)にする
type LocalClaims struct {
	jwt.RegisteredClaims
	Name  string   `json:"name"`
	Email string   `json:"email"`
	Roles []string `json:"roles"`
}

func newLocalClaims(issuer string, userID uint64, name, email string, roles []string) LocalClaims {
	now := time.Now()
	return LocalClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    issuer,
			Subject:   strconv.FormatUint(userID, 10),
			Audience:  jwt.ClaimStrings{localTokenAudience},
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(localTokenTTL)),
		},
		Name:  name,
		Email: email,
		Roles: roles,
	}
}

// IssueLocalHMACToken は `iss=bff-gin-local-hmac` のHS256署名JWTを発行する
func IssueLocalHMACToken(secret string, userID uint64, name, email string, roles []string) (tokenString string, exp time.Time, err error) {
	claims := newLocalClaims(LocalHMACIssuer, userID, name, email, roles)
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString([]byte(secret))
	if err != nil {
		return "", time.Time{}, fmt.Errorf("ローカルJWT(HMAC)の署名に失敗しました: %w", err)
	}
	return signed, claims.ExpiresAt.Time, nil
}

// LocalRSAKeyPair はbffが起動時に1回だけ生成し、プロセスの生存期間だけ保持するRSA鍵ペア
// CONTRACT.mdセクション16.4: 永続化しない(再起動でRSAログインの
// 既存セッションは検証不能になり再ログインが必要になるが、学習用途としての割り切り
// 本番では鍵の永続化・kidによるローテーションが必要)
type LocalRSAKeyPair struct {
	private *rsa.PrivateKey
}

// NewLocalRSAKeyPair はRSA-2048の鍵ペアを新規生成する
func NewLocalRSAKeyPair() (*LocalRSAKeyPair, error) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, fmt.Errorf("ローカル認証用RSA鍵ペアの生成に失敗しました: %w", err)
	}
	return &LocalRSAKeyPair{private: key}, nil
}

// IssueToken は `iss=bff-gin-local-rsa` のRS256署名JWTを発行する
func (kp *LocalRSAKeyPair) IssueToken(userID uint64, name, email string, roles []string) (tokenString string, exp time.Time, err error) {
	claims := newLocalClaims(LocalRSAIssuer, userID, name, email, roles)
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["kid"] = localRSAKeyID
	signed, err := token.SignedString(kp.private)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("ローカルJWT(RSA)の署名に失敗しました: %w", err)
	}
	return signed, claims.ExpiresAt.Time, nil
}

// PublicKey は保持しているRSA鍵ペアの公開鍵部分を返す
// テストや、JWKS 以外の経路でこの鍵を使って検証したい場合向けのアクセサ
func (kp *LocalRSAKeyPair) PublicKey() *rsa.PublicKey {
	return &kp.private.PublicKey
}

// jwksKey は JWKS 1件分の JSON 表現
// backend/internal/authjwt/jwks.go の jwk 型と
// 互換の形式にする(kty=RSA、n/eはbase64url無パディング)
type jwksKey struct {
	Kty string `json:"kty"`
	Kid string `json:"kid"`
	Alg string `json:"alg"`
	Use string `json:"use"`
	N   string `json:"n"`
	E   string `json:"e"`
}

// JWKS は公開鍵を JWKS 形式(`{"keys":[...]}`)で返す
// `GET /.well-known/jwks.json` から使う
func (kp *LocalRSAKeyPair) JWKS() map[string]any {
	pub := kp.private.PublicKey
	n := base64.RawURLEncoding.EncodeToString(pub.N.Bytes())
	e := base64.RawURLEncoding.EncodeToString(bigEndianBytes(pub.E))
	return map[string]any{
		"keys": []jwksKey{
			{Kty: "RSA", Kid: localRSAKeyID, Alg: "RS256", Use: "sig", N: n, E: e},
		},
	}
}

// bigEndianBytes はrsa.PublicKey.E(int)をJWKの `e` フィールドが要求する
// big-endianバイト列に変換する(通常65537 = 0x010001の3バイト)
func bigEndianBytes(e int) []byte {
	if e == 0 {
		return []byte{0}
	}
	var b []byte
	for e > 0 {
		b = append([]byte{byte(e & 0xff)}, b...)
		e >>= 8
	}
	return b
}
