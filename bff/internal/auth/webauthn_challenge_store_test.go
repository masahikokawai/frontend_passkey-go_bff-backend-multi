package auth

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/redis/go-redis/v9"
)

func newTestWebauthnChallengeStore(t *testing.T) *WebauthnChallengeStore {
	t.Helper()
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { client.Close() })
	return NewWebauthnChallengeStore(client)
}

func TestWebauthnChallengeStore_SaveThenTake_RoundTrips(t *testing.T) {
	store := newTestWebauthnChallengeStore(t)
	ctx := context.Background()
	want := &webauthn.SessionData{Challenge: "chal-1", UserID: []byte("user-1")}

	if err := store.Save(ctx, "login:state-1", want); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	got, err := store.Take(ctx, "login:state-1")
	if err != nil {
		t.Fatalf("Take() error = %v", err)
	}
	if got.Challenge != want.Challenge {
		t.Errorf("Challenge = %q, want %q", got.Challenge, want.Challenge)
	}
}

// TestWebauthnChallengeStore_Take_IsOneTimeUse は「一度取り出したchallengeは
// 二度と取り出せない(=リプレイに使えない)」という設計上の前提そのものを確認する
func TestWebauthnChallengeStore_Take_IsOneTimeUse(t *testing.T) {
	store := newTestWebauthnChallengeStore(t)
	ctx := context.Background()
	if err := store.Save(ctx, "login:state-1", &webauthn.SessionData{Challenge: "chal-1"}); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	if _, err := store.Take(ctx, "login:state-1"); err != nil {
		t.Fatalf("1回目のTake() error = %v, want nil", err)
	}
	if _, err := store.Take(ctx, "login:state-1"); err != ErrWebauthnChallengeNotFound {
		t.Errorf("2回目のTake() error = %v, want ErrWebauthnChallengeNotFound(既に消費済みのchallengeが再利用できてしまっている)", err)
	}
}

// TestWebauthnChallengeStore_Take_ConcurrentCallsOnlySucceedOnce は
// セキュリティ監査(3回目)で発見したTOCTOU競合状態の回帰テスト
//
// 【修正前の状態】
// Take()は`GET`→`DEL`という2つの別々のRedisコマンドで実装されていた
// 同じkeyに対して2つのリクエストがほぼ同時に到着すると(リプレイ攻撃者が同一の
// WebAuthnアサーションを短時間に2回送りつけるケースを想定)、両方が削除前の`GET`に
// 成功してしまい、「一度きりのchallenge」という前提が崩れ、同じ儀式が2回成立してしまう可能性があった
// GetDel(Redisの原子的な単一コマンド)に統一することで、
// 2つの並行呼び出しのうち必ず片方だけが成功するようになったことをここで確認する
func TestWebauthnChallengeStore_Take_ConcurrentCallsOnlySucceedOnce(t *testing.T) {
	store := newTestWebauthnChallengeStore(t)
	ctx := context.Background()
	if err := store.Save(ctx, "login:state-1", &webauthn.SessionData{Challenge: "chal-1"}); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	const concurrency = 500
	var succeeded int32
	var wg sync.WaitGroup
	wg.Add(concurrency)
	for i := 0; i < concurrency; i++ {
		go func() {
			defer wg.Done()
			if _, err := store.Take(ctx, "login:state-1"); err == nil {
				atomic.AddInt32(&succeeded, 1)
			}
		}()
	}
	wg.Wait()

	if succeeded != 1 {
		t.Errorf("成功したTake()呼び出し数 = %d, want 1(challengeが複数回消費できてしまっている=リプレイ攻撃が成立する)", succeeded)
	}
}

func TestWebauthnChallengeStore_Take_UnknownKey_ReturnsNotFound(t *testing.T) {
	store := newTestWebauthnChallengeStore(t)
	if _, err := store.Take(context.Background(), "login:does-not-exist"); err != ErrWebauthnChallengeNotFound {
		t.Errorf("error = %v, want ErrWebauthnChallengeNotFound", err)
	}
}
