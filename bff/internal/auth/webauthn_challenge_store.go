package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/redis/go-redis/v9"
)

// ErrWebauthnChallengeNotFound はRedisに一時保存したchallengeが見つからない場合
// (TTL切れ、または存在しない state/session_id を指定された場合)
var ErrWebauthnChallengeNotFound = errors.New("webauthn_challenge_not_found")

// webauthnChallengeTTL はBeginRegistration/BeginLogin〜Finish*までの許容時間
// go-webauthnの既定challenge timeoutと同程度で十分なため5分とする
const webauthnChallengeTTL = 5 * time.Minute

// WebauthnChallengeStore はwebauthn.SessionData(登録/ログインの儀式で発行した
// challenge)を、Finish*が呼ばれるまでRedisに一時保存する
// セッションCookie(auth.Store)とは別物: こちらは儀式1回ぶんの使い捨てデータで、
// ログイン前(session_idが無い状態)でも使えるよう任意のキー文字列で出し入れする
type WebauthnChallengeStore struct {
	redis *redis.Client
}

func NewWebauthnChallengeStore(redisClient *redis.Client) *WebauthnChallengeStore {
	return &WebauthnChallengeStore{redis: redisClient}
}

func webauthnChallengeKey(key string) string {
	return "webauthn_challenge:" + key
}

// Save はkeyに紐づけてSessionDataを保存する
// 登録時はkey="reg:{session_id}"、ログイン時はkey="login:{ランダムなstate}"を使う想定
func (s *WebauthnChallengeStore) Save(ctx context.Context, key string, data *webauthn.SessionData) error {
	payload, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("webauthn challengeのシリアライズに失敗しました: %w", err)
	}
	if err := s.redis.Set(ctx, webauthnChallengeKey(key), payload, webauthnChallengeTTL).Err(); err != nil {
		return fmt.Errorf("webauthn challengeの保存に失敗しました: %w", err)
	}
	return nil
}

// Take はSessionDataを取得したうえで即座に削除する(1回きりのchallengeのため)
//
// 【セキュリティ監査(3回目)で発見・修正】以前はGET→DELという2回の別々のRedisコマンドで
// 実装されており、同じkeyに対する2つの並行リクエスト(例: リプレイ攻撃者が同じ
// WebAuthnアサーションを短時間に2回送りつける)が、どちらも削除される前のGETに成功してしまい、
// 「一度きりのchallenge」という前提が崩れるTOCTOU競合状態があった(oidc.goのpendingAuthは
// 同じ目的でRedisのGETDEL(単一の原子的コマンド)を最初から使っており、この点で一貫していなかった)
// GetDelに統一することで、GET+DELが1つの原子操作になり、2つの並行リクエストのうち
// 片方だけが確実にchallengeを取得できるようになる
func (s *WebauthnChallengeStore) Take(ctx context.Context, key string) (*webauthn.SessionData, error) {
	fullKey := webauthnChallengeKey(key)
	raw, err := s.redis.GetDel(ctx, fullKey).Result()
	if errors.Is(err, redis.Nil) {
		return nil, ErrWebauthnChallengeNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("webauthn challengeの取得に失敗しました: %w", err)
	}

	var data webauthn.SessionData
	if err := json.Unmarshal([]byte(raw), &data); err != nil {
		return nil, fmt.Errorf("webauthn challengeのデシリアライズに失敗しました: %w", err)
	}
	return &data, nil
}
