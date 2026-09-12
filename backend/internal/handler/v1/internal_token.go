package v1

import "crypto/subtle"

// secureTokenEqual は共有シークレットの比較に使う定数時間比較
//
// 【3回目のテスト監査(認可観点)で発覚】RequireAdminInternalToken・RequireFeatureFlagPollToken・
// RequireLocalAuthInternalToken・RequireWebauthnInternalTokenの4つはいずれも`!=`による
// 素朴な文字列比較で共有シークレットを検証していた
//
// Goの文字列比較は先頭から不一致が
// 見つかった時点で処理を打ち切るため、理論上は応答時間の差から正しいトークンを
// 1文字ずつ推測されるタイミング攻撃の対象になりうる(学習用プロジェクトのdev環境
// 固定シークレットとはいえ、共有シークレット比較の定石として直しておく価値がある)
//
// crypto/subtle.ConstantTimeCompareは長さの違いを跨いだ比較を保証しないため、
// 先に長さを見て違えば早期にfalseを返してよい(長さ自体は秘密ではない)
func secureTokenEqual(got, expected string) bool {
	if len(got) != len(expected) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(got), []byte(expected)) == 1
}
