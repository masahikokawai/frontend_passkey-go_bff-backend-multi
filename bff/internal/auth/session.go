package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// ErrSessionNotFound はセッションIDに対応するデータがRedisに存在しない場合
// Cookieはあるがセッション切れ・ログアウト済みなどで発生する
var ErrSessionNotFound = errors.New("session not found")

// ErrLockNotAcquired は分散ロックを既定回数リトライしても取得できなかった場合
var ErrLockNotAcquired = errors.New("failed to acquire session lock")

// Session は Redis に保存する内容
// ブラウザにはこの中身は一切渡らず、session_id Cookie という「不透明な鍵」だけが渡る(BFFパターンの核心)
type Session struct {
	UserID      uint64   `json:"user_id"`
	KeycloakSub string   `json:"keycloak_sub"`
	Name        string   `json:"name"`
	Email       string   `json:"email"`
	Roles       []string `json:"roles"`
	// AuthMode は "keycloak" / "local_hmac" / "local_rsa" のいずれか
	// (CONTRACT.mdセクション16.4)
	// ログアウト時にRP-Initiated Logoutを行うかどうか、
	// およびリアクティブリフレッシュ(Refresher.Do)で refresh_token による更新を
	// 試みてよいかどうかを、この値で分岐する
	AuthMode        string    `json:"auth_mode"`
	AccessToken     string    `json:"access_token"`
	RefreshToken    string    `json:"refresh_token"`
	IDToken         string    `json:"id_token"`
	AccessTokenExp  time.Time `json:"access_token_exp"`
	RefreshTokenExp time.Time `json:"refresh_token_exp"`
}

const (
	AuthModeKeycloak  = "keycloak"
	AuthModeLocalHMAC = "local_hmac"
	AuthModeLocalRSA  = "local_rsa"
	// AuthModePasskey はWebAuthn(パスキー)ログイン成功時のセッション(CONTRACT.mdセクション22)
	// ローカル認証ユーザー専用の追加認証手段のため、実体はAuthModeLocalHMACと同じ
	// HS256署名JWTを発行するが、`Refresher.Do`でのリフレッシュ対象外判定・admin画面での
	// 表示等のために区別できる値を独立して持たせる
	AuthModePasskey = "passkey"
)

// Store はRedisをバックエンドとするセッションストア
// CONTRACT.md: `session:{sessionID}` / `lock:session:{sessionID}`
type Store struct {
	redis *redis.Client
}

func NewStore(redisClient *redis.Client) *Store {
	return &Store{redis: redisClient}
}

func sessionKey(sessionID string) string {
	return "session:" + sessionID
}

func lockKey(sessionID string) string {
	return "lock:session:" + sessionID
}

// Create は新規セッションを発行し、生成したsessionIDを返す
// TTLはRefreshTokenExpに追従させる(refresh tokenが失効するタイミングでRedis上の
// セッションも自然に消える)
func (s *Store) Create(ctx context.Context, sess Session) (string, error) {
	sessionID, err := randomToken(32)
	if err != nil {
		return "", fmt.Errorf("session_idの生成に失敗しました: %w", err)
	}
	if err := s.save(ctx, sessionID, sess); err != nil {
		return "", err
	}
	return sessionID, nil
}

// Get はセッションを取得する
// 見つからない場合は ErrSessionNotFound を返す
func (s *Store) Get(ctx context.Context, sessionID string) (*Session, error) {
	raw, err := s.redis.Get(ctx, sessionKey(sessionID)).Result()
	if errors.Is(err, redis.Nil) {
		return nil, ErrSessionNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("セッション取得に失敗しました: %w", err)
	}
	var sess Session
	if err := json.Unmarshal([]byte(raw), &sess); err != nil {
		return nil, fmt.Errorf("セッションのデシリアライズに失敗しました: %w", err)
	}
	return &sess, nil
}

// Update は既存セッションをリフレッシュ後の内容で上書きし、TTLも再設定する
func (s *Store) Update(ctx context.Context, sessionID string, sess Session) error {
	return s.save(ctx, sessionID, sess)
}

func (s *Store) save(ctx context.Context, sessionID string, sess Session) error {
	payload, err := json.Marshal(sess)
	if err != nil {
		return fmt.Errorf("セッションのシリアライズに失敗しました: %w", err)
	}
	ttl := time.Until(sess.RefreshTokenExp)
	if ttl <= 0 {
		// 期限切れのトークンをそのまま保存するのは事故のもとなので拒否する
		return fmt.Errorf("refresh_token_expが既に過去の時刻です: %s", sess.RefreshTokenExp)
	}
	if err := s.redis.Set(ctx, sessionKey(sessionID), payload, ttl).Err(); err != nil {
		return fmt.Errorf("セッション保存に失敗しました: %w", err)
	}
	return nil
}

// Delete はログアウト時にセッションを破棄する
func (s *Store) Delete(ctx context.Context, sessionID string) error {
	if err := s.redis.Del(ctx, sessionKey(sessionID)).Err(); err != nil {
		return fmt.Errorf("セッション削除に失敗しました: %w", err)
	}
	return nil
}

// lockRetryInterval / lockMaxWait はロック取得待ちのポーリング間隔と上限 BFF は複数インスタンスで動くステートレス構成のため、
// 同一セッションに対する同時リクエストがそれぞれ「access token期限切れ」を検知してリフレッシュを競合実行すると、
// Keycloakのrefresh token rotationにより後発のリフレッシュが失敗し、セッションが不必要に無効化されてしまう
// これを防ぐための分散ロック
const (
	lockTTL           = 5 * time.Second
	lockRetryInterval = 50 * time.Millisecond
	lockMaxWait       = 3 * time.Second
)

// WithLock は sessionID に対する分散ロックを取得したうえで fn を実行する既に他プロセス/他ゴルーチンがロックを保持している場合は、
// 解放されるまで短い間隔でポーリングして待つ
// lockMaxWait を超えても取得できなければ ErrLockNotAcquired を返す
func (s *Store) WithLock(ctx context.Context, sessionID string, fn func(ctx context.Context) error) error {
	lockToken, err := randomToken(16)
	if err != nil {
		return fmt.Errorf("lock tokenの生成に失敗しました: %w", err)
	}

	deadline := time.Now().Add(lockMaxWait)
	for {
		ok, err := s.redis.SetNX(ctx, lockKey(sessionID), lockToken, lockTTL).Result()
		if err != nil {
			return fmt.Errorf("ロック取得に失敗しました: %w", err)
		}
		if ok {
			break
		}
		if time.Now().After(deadline) {
			return ErrLockNotAcquired
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(lockRetryInterval):
		}
	}

	defer func() {
		// 自分が取得したロックだけを解放する(トークンが一致する場合のみDEL)
		// TTLが切れて他者が既に新しいロックを取っているケースで誤って消さないため
		script := redis.NewScript(`
			if redis.call("GET", KEYS[1]) == ARGV[1] then
				return redis.call("DEL", KEYS[1])
			end
			return 0
		`)
		script.Run(ctx, s.redis, []string{lockKey(sessionID)}, lockToken)
	}()

	return fn(ctx)
}
