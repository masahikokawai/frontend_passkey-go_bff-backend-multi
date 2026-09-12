// Package authjwt はKeycloakが発行したJWT(Access Token)の検証を担う
// REST v1(Ginミドルウェア)・gRPC v2(interceptor)の両方から共通で使う
// (CONTRACT.mdセクション5「JWT検証はREST/gRPC共通実装にする」)
//
// Rails対比: Rails版はセッションCookieに `current_user_id` を積むだけで
// 署名検証は不要だった(RailsのCookieStoreがサーバー側の秘密鍵で署名済み)
// OIDC/JWT方式ではトークンの発行者(Keycloak)とトークンの利用者(backend)が
// 別プロセスになるため、「本当にKeycloakが発行したものか」をJWKSの公開鍵で
// 検証するステップが新たに必要になる
package authjwt

import (
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Claims はKeycloakのID/Access TokenのペイロードのうちAPI側で使う分だけを表す
type Claims struct {
	jwt.RegisteredClaims
	PreferredUsername string      `json:"preferred_username"`
	Email             string      `json:"email"`
	Name              string      `json:"name"`
	RealmAccess       RealmAccess `json:"realm_access"`
	// Azp(authorized party)はトークンを発行してもらったクライアントのID
	// 外部API(RequireExternalClientAuth)がClient Credentials Grantで発行された
	// トークンかどうかを見分けるために使う(CONTRACT.mdセクション11)
	Azp string `json:"azp"`
}

type RealmAccess struct {
	Roles []string `json:"roles"`
}

// jwk はJWKS(JSON Web Key Set)1件分
// RSA公開鍵のみ扱う(Keycloakの既定)
type jwk struct {
	Kty string `json:"kty"`
	Kid string `json:"kid"`
	Alg string `json:"alg"`
	Use string `json:"use"`
	N   string `json:"n"`
	E   string `json:"e"`
}

type jwksResponse struct {
	Keys []jwk `json:"keys"`
}

// Verifier はJWKSを取得・キャッシュし、JWTを検証する
//
// kid(鍵ID)ごとにrsa.PublicKeyをメモリキャッシュし、未知のkidが来たときだけ
// JWKSを再取得する(Keycloakの鍵ローテーションに追従しつつ、毎リクエストで
// JWKSエンドポイントを叩かないようにするため)
type Verifier struct {
	jwksURL  string
	issuer   string
	audience string

	httpClient *http.Client

	mu   sync.RWMutex
	keys map[string]*rsa.PublicKey
}

func NewVerifier(jwksURL, issuer, audience string) *Verifier {
	return &Verifier{
		jwksURL:    jwksURL,
		issuer:     issuer,
		audience:   audience,
		httpClient: &http.Client{Timeout: 5 * time.Second},
		keys:       make(map[string]*rsa.PublicKey),
	}
}

// Verify はAuthorizationヘッダから取り出した生のJWT文字列を検証する
// iss/aud/exp/nbf/alg=RS256をすべて満たした場合のみClaimsを返す
func (v *Verifier) Verify(tokenString string) (*Claims, error) {
	claims := &Claims{}
	_, err := jwt.ParseWithClaims(tokenString, claims, v.keyFunc,
		jwt.WithValidMethods([]string{"RS256"}),
		jwt.WithIssuer(v.issuer),
		jwt.WithAudience(v.audience),
	)
	if err != nil {
		return nil, fmt.Errorf("JWT検証失敗: %w", err)
	}
	return claims, nil
}

// keyFunc はgolang-jwt/jwt/v5が要求するKeyfuncの実装
// token.Header["kid"]でどの公開鍵を使うか判断する
func (v *Verifier) keyFunc(token *jwt.Token) (any, error) {
	kid, ok := token.Header["kid"].(string)
	if !ok || kid == "" {
		return nil, fmt.Errorf("JWTヘッダにkidが無い")
	}

	if key := v.lookup(kid); key != nil {
		return key, nil
	}

	// キャッシュに無いkid → JWKSを1回だけ再取得して再確認する(鍵ローテーション対応)
	if err := v.refresh(); err != nil {
		return nil, fmt.Errorf("JWKS取得失敗: %w", err)
	}
	if key := v.lookup(kid); key != nil {
		return key, nil
	}
	return nil, fmt.Errorf("kid=%s に対応する公開鍵が見つからない", kid)
}

func (v *Verifier) lookup(kid string) *rsa.PublicKey {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return v.keys[kid]
}

// Prefetch は起動時に一度だけJWKSを取得しておくためのメソッド
func (v *Verifier) Prefetch() error {
	return v.refresh()
}

func (v *Verifier) refresh() error {
	resp, err := v.httpClient.Get(v.jwksURL)
	if err != nil {
		return fmt.Errorf("JWKSエンドポイントへのリクエスト失敗: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("JWKSレスポンス読み込み失敗: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("JWKSエンドポイントが異常なステータスを返した: %d body=%s", resp.StatusCode, body)
	}

	var parsed jwksResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return fmt.Errorf("JWKS JSONのパース失敗: %w", err)
	}

	newKeys := make(map[string]*rsa.PublicKey, len(parsed.Keys))
	for _, k := range parsed.Keys {
		if k.Kty != "RSA" || (k.Use != "" && k.Use != "sig") {
			continue
		}
		pub, err := jwkToRSAPublicKey(k)
		if err != nil {
			continue
		}
		newKeys[k.Kid] = pub
	}

	v.mu.Lock()
	v.keys = newKeys
	v.mu.Unlock()
	return nil
}

// jwkToRSAPublicKey はJWKの n(modulus)/e(exponent)フィールド(base64url、パディング無し)
// からrsa.PublicKeyを組み立てる
//
// Go標準ライブラリには「JWKをパースしてPublicKeyにする」関数が無いため、
// JSONの数値表現(base64urlエンコードされた大きな整数)をmath/bigで自分で復元する必要がある
// これはGoが「小さな標準ライブラリを組み合わせて使う」思想であることの一例
func jwkToRSAPublicKey(k jwk) (*rsa.PublicKey, error) {
	nBytes, err := base64.RawURLEncoding.DecodeString(k.N)
	if err != nil {
		return nil, fmt.Errorf("modulus(n)のデコード失敗: %w", err)
	}
	eBytes, err := base64.RawURLEncoding.DecodeString(k.E)
	if err != nil {
		return nil, fmt.Errorf("exponent(e)のデコード失敗: %w", err)
	}

	n := new(big.Int).SetBytes(nBytes)
	e := new(big.Int).SetBytes(eBytes)

	return &rsa.PublicKey{
		N: n,
		E: int(e.Int64()),
	}, nil
}
