package auth

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/google/go-cmp/cmp"
	"github.com/redis/go-redis/v9"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { client.Close() })
	return NewStore(client)
}

func TestStore_CreateGetUpdateDelete(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	sess := Session{
		UserID:          1,
		KeycloakSub:     "sub-1",
		Name:            "Taro",
		Email:           "taro@example.com",
		AccessToken:     "access-1",
		RefreshToken:    "refresh-1",
		IDToken:         "id-1",
		AccessTokenExp:  time.Now().Add(time.Minute),
		RefreshTokenExp: time.Now().Add(time.Hour),
	}

	sessionID, err := store.Create(ctx, sess)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if sessionID == "" {
		t.Fatal("Create() returned empty sessionID")
	}

	got, err := store.Get(ctx, sessionID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	// AccessTokenExp/RefreshTokenExpはJSON経由でナノ秒精度が変わるため個別に比較する
	if diff := cmp.Diff(sess.UserID, got.UserID); diff != "" {
		t.Errorf("UserID mismatch (-want +got):\n%s", diff)
	}
	if got.AccessToken != sess.AccessToken {
		t.Errorf("AccessToken = %q, want %q", got.AccessToken, sess.AccessToken)
	}

	updated := *got
	updated.AccessToken = "access-2"
	updated.RefreshTokenExp = time.Now().Add(2 * time.Hour)
	if err := store.Update(ctx, sessionID, updated); err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	got2, err := store.Get(ctx, sessionID)
	if err != nil {
		t.Fatalf("Get() after update error = %v", err)
	}
	if got2.AccessToken != "access-2" {
		t.Errorf("AccessToken after update = %q, want %q", got2.AccessToken, "access-2")
	}

	if err := store.Delete(ctx, sessionID); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if _, err := store.Get(ctx, sessionID); !errors.Is(err, ErrSessionNotFound) {
		t.Errorf("Get() after delete error = %v, want ErrSessionNotFound", err)
	}
}

func TestStore_Create_RejectsAlreadyExpiredRefreshToken(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	sess := Session{RefreshTokenExp: time.Now().Add(-time.Minute)}
	if _, err := store.Create(ctx, sess); err == nil {
		t.Fatal("Create() with past RefreshTokenExp should return an error, got nil")
	}
}

// TestStore_WithLock_ExclusiveAccess は、同一 sessionID に対する複数ゴルーチンからの WithLock 呼び出しが直列化されることを検証する
// 分散ロックが機能していない場合、counter++ が競合しレース検出やカウント不一致として顕在化する
func TestStore_WithLock_ExclusiveAccess(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	const sessionID = "session-under-test"
	const goroutines = 20

	var counter int64
	var wg sync.WaitGroup
	var lockNotAcquired int64

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := store.WithLock(ctx, sessionID, func(ctx context.Context) error {
				// ロック内でしか安全に更新できない値を、あえてread→sleep→writeの
				// 順で操作し、排他されていなければ容易にレースが起きるようにする
				current := atomic.LoadInt64(&counter)
				time.Sleep(time.Millisecond)
				atomic.StoreInt64(&counter, current+1)
				return nil
			})
			if errors.Is(err, ErrLockNotAcquired) {
				atomic.AddInt64(&lockNotAcquired, 1)
				return
			}
			if err != nil {
				t.Errorf("WithLock() unexpected error = %v", err)
			}
		}()
	}
	wg.Wait()

	want := goroutines - int(atomic.LoadInt64(&lockNotAcquired))
	if int(counter) != want {
		t.Errorf("counter = %d, want %d (ロックが排他できておらず更新が失われた可能性がある)", counter, want)
	}
}
