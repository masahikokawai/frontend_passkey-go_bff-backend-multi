package authjwt

import (
	"fmt"

	"github.com/golang-jwt/jwt/v5"
)

// LocalHMACIssuer/LocalRSAIssuer はbffが自分で発行するローカルJWTの`iss`固定値
// (CONTRACT.md セクション16.4)
// bff側もこれと同じ文字列をissとして設定する
const (
	LocalHMACIssuer = "bff-gin-local-hmac"
	LocalRSAIssuer  = "bff-gin-local-rsa"
)

// IsLocalIssuer はclaims.Issuerがローカル発行(HMAC/RSAいずれか)かどうかを返す
// v1/task.go・grpcserver/task_service.goのresolveUserIDが、Keycloak発行トークン
// (sub=keycloak_sub)とローカル発行トークン(sub=内部user_idそのもの)を
// 区別するために使う(CONTRACT.mdセクション16.5)
func IsLocalIssuer(iss string) bool {
	return iss == LocalHMACIssuer || iss == LocalRSAIssuer
}

// TokenVerifier はJWT検証の最小インターフェース
// *Verifier(JWKS/RSA)・*HMACVerifier・*Dispatcher のいずれもこれを満たす
type TokenVerifier interface {
	Verify(tokenString string) (*Claims, error)
}

// Dispatcher はJWTの`iss`クレームで検証方式(Keycloak/ローカルHMAC/ローカルRSA)を
// 振り分ける(CONTRACT.md セクション16.5)
//
// 【設計上の要点】ParseUnverifiedで覗いたissは署名検証前の値であり、誰でも自由に書き換えられる
// ここではその値を「どのverifierに委譲するか」を決める振り分け
// キーとしてのみ使い、実際の信頼(本当にそのissuerが署名したか)は委譲先の verifier.Verifyが行う署名検証に委ねる
// この2段階構造により、未知の iss を詐称しても対応するverifierが存在しないため即エラーになり、
// 既知のissを詐称しても対応するverifierの署名検証で弾かれる
type Dispatcher struct {
	byIssuer map[string]TokenVerifier
}

func NewDispatcher() *Dispatcher {
	return &Dispatcher{byIssuer: make(map[string]TokenVerifier)}
}

// Register はissuer文字列とverifierの対応を登録する
// メソッドチェーンできるよう自分自身を返す
func (d *Dispatcher) Register(issuer string, verifier TokenVerifier) *Dispatcher {
	d.byIssuer[issuer] = verifier
	return d
}

func (d *Dispatcher) Verify(tokenString string) (*Claims, error) {
	var peek jwt.RegisteredClaims
	if _, _, err := jwt.NewParser().ParseUnverified(tokenString, &peek); err != nil {
		return nil, fmt.Errorf("JWTのパースに失敗(iss確認前): %w", err)
	}
	verifier, ok := d.byIssuer[peek.Issuer]
	if !ok {
		return nil, fmt.Errorf("不明なissuer: %q", peek.Issuer)
	}
	return verifier.Verify(tokenString)
}
