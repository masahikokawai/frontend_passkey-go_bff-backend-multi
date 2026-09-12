package auth

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// fakeTokenRefresher はKeycloakなしでRefresher.Doのリトライ挙動を検証するためのstub
type fakeTokenRefresher struct {
	calls int
	err   error
}

func (f *fakeTokenRefresher) Refresh(_ context.Context, _ string) (*TokenSet, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	return &TokenSet{
		AccessToken:     "new-access-token",
		RefreshToken:    "new-refresh-token",
		IDToken:         "new-id-token",
		AccessTokenExp:  time.Now().Add(time.Minute),
		RefreshTokenExp: time.Now().Add(time.Hour),
	}, nil
}

// slowFakeTokenRefresher はRefresh呼び出しにdelay分の遅延を挟む
// TestRefresher_Do_ConcurrentRefresh_SecondCallerReusesFreshTokenで、
// 「1つ目のゴルーチンがロックを保持しリフレッシュ中の間に、2つ目のゴルーチンが
// ロック待ちの状態になる」という状況を確実に作るために使う
//
// 【テストコード監査(並行性テスト自体の決定性)で発見・修正】以前はテスト側で
// `time.Sleep(20*time.Millisecond)`を「ゴルーチンAが先にRefreshへ入るのを
// 待つおおよその時間」として使っていたが、これはCPU負荷の高い環境
// (CI等)ではAの開始自体が20msより遅れる可能性があり、原理的にフレーキーな
// 同期方法だった(このプロジェクトの安全性チェック文書でも、`time.Sleep`ベースの
// 同期より明示的なシグナル/バリアを使う方が望ましいとされている)。
// startedチャンネルへ「Refreshに実際に入った瞬間」を通知することで、
// 呼び出し側は決め打ちの時間ではなく実際の到達を待てるようにした
type slowFakeTokenRefresher struct {
	fakeTokenRefresher
	delay   time.Duration
	started chan struct{} // Refresh呼び出しが開始された瞬間に1回だけ送信される(容量1)
}

func (f *slowFakeTokenRefresher) Refresh(ctx context.Context, refreshToken string) (*TokenSet, error) {
	if f.started != nil {
		select {
		case f.started <- struct{}{}:
		default:
		}
	}
	time.Sleep(f.delay)
	return f.fakeTokenRefresher.Refresh(ctx, refreshToken)
}

// TestRefresher_Do_ConcurrentRefresh_SecondCallerReusesFreshToken は
// テストコード監査(2回目)で見つかった穴の解消: middleware.goのDo()には
// 「ロック取得中に他のリクエストが既にリフレッシュ済みだった場合、
// tokenRefresh.Refreshをもう一度呼ばずに最新のaccess tokenで再試行する」という分岐
// (latest.AccessToken != sess.AccessToken の分岐)があるが、これまで
// session_test.goのTestStore_WithLock_ExclusiveAccessは素のWithLockの排他性しか
// 検証しておらず、Refresher.Do自体を2並行で呼んだ場合にこの分岐が実際に機能し、
// Keycloakへの重複リフレッシュリクエストを防げているかは未検証だった
// (CONTRACT.mdセクション2の「同一セッションへの同時リフレッシュ競合を防ぐ」という
// 設計意図そのものの検証)
func TestRefresher_Do_ConcurrentRefresh_SecondCallerReusesFreshToken(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	sessionID, err := store.Create(ctx, Session{
		AuthMode:        AuthModeKeycloak,
		AccessToken:     "old-access-token",
		RefreshToken:    "old-refresh-token",
		RefreshTokenExp: time.Now().Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("store.Create() error = %v", err)
	}

	refresher := &slowFakeTokenRefresher{delay: 150 * time.Millisecond, started: make(chan struct{}, 1)}
	r := NewRefresher(store, refresher)

	var wg sync.WaitGroup
	var muA, muB sync.Mutex
	var aFnCalls, bFnCalls []string // 各呼び出しで渡されたaccess tokenを記録する

	wg.Add(2)
	// ゴルーチンA: 先にDo()へ入り、ロックを取得してリフレッシュを開始する(150ms保持)
	go func() {
		defer wg.Done()
		err := r.Do(ctx, sessionID, func(accessToken string) error {
			muA.Lock()
			aFnCalls = append(aFnCalls, accessToken)
			muA.Unlock()
			if accessToken == "old-access-token" {
				return ErrUpstreamUnauthorized
			}
			return nil
		})
		if err != nil {
			t.Errorf("goroutine A: Do() error = %v", err)
		}
	}()

	// ゴルーチンB: Aがロックを取得しRefresh呼び出しへ実際に入ったこと(=リフレッシュ処理中で
	// あることが確定した瞬間)を待ってから追いかけて同じセッションでDo()を呼ぶ。
	// 以前はここが`time.Sleep(20*time.Millisecond)`という決め打ちの時間待ちだったが、
	// startedチャンネルによる実際のシグナル待ちに変えることで、実行環境の速度に
	// 依存しない決定的なテストにした(150ms遅延中にBが必ずold-access-tokenを
	// 読み込める、という前提はstartedを受け取った直後なら常に成立する)
	select {
	case <-refresher.started:
	case <-time.After(5 * time.Second):
		t.Fatal("goroutine Aが5秒以内にRefreshへ到達しなかった(デッドロックの疑い)")
	}
	go func() {
		defer wg.Done()
		err := r.Do(ctx, sessionID, func(accessToken string) error {
			muB.Lock()
			bFnCalls = append(bFnCalls, accessToken)
			muB.Unlock()
			if accessToken == "old-access-token" {
				return ErrUpstreamUnauthorized
			}
			return nil
		})
		if err != nil {
			t.Errorf("goroutine B: Do() error = %v", err)
		}
	}()

	wg.Wait()

	// 【本題】tokenRefresh.Refreshは1回しか呼ばれていないこと
	// (Bがロック待ちの間にAのリフレッシュ結果を横取りできず、自分でも
	// Keycloakへリフレッシュリクエストを重複送信してしまうと、ここが2になる)
	if refresher.calls != 1 {
		t.Errorf("Refresh call count = %d, want 1(2回リフレッシュされるとKeycloakのrefresh token rotationで片方が失効しうる)", refresher.calls)
	}

	// Bも最終的には新しいaccess tokenで成功しているはず
	if len(bFnCalls) != 2 || bFnCalls[1] != "new-access-token" {
		t.Errorf("goroutine Bのfn呼び出し = %v, want [old-access-token new-access-token]", bFnCalls)
	}
	if len(aFnCalls) != 2 || aFnCalls[1] != "new-access-token" {
		t.Errorf("goroutine Aのfn呼び出し = %v, want [old-access-token new-access-token]", aFnCalls)
	}
}

func TestRefresher_Do_TableDriven(t *testing.T) {
	tests := []struct {
		name          string
		fnErr         error
		refreshErr    error
		authMode      string
		wantErr       bool
		wantErrIs     error
		wantFnCalls   int
		wantRefreshed bool
	}{
		{
			name:        "成功時はそのまま1回だけ呼ばれリフレッシュは発生しない",
			fnErr:       nil,
			wantFnCalls: 1,
		},
		{
			name:          "401なら1回リフレッシュしてリトライが成功する",
			fnErr:         ErrUpstreamUnauthorized,
			wantFnCalls:   2,
			wantRefreshed: true,
		},
		{
			name:        "401以外のエラーはリフレッシュせずそのまま返す",
			fnErr:       errors.New("boom"),
			wantErr:     true,
			wantFnCalls: 1,
		},
		{
			name:       "リフレッシュ自体が失敗したらセッションを破棄しエラーを返す",
			fnErr:      ErrUpstreamUnauthorized,
			refreshErr: errors.New("refresh token expired"),
			wantErr:    true,
		},
		{
			// CONTRACT.mdセクション16.4: ローカル認証(HMAC/RSA)にはrefresh_tokenが
			// 無いため、401を受けてもリフレッシュを試みずセッションを破棄する
			name:        "ローカル認証セッションは401でリフレッシュを試みずセッションを破棄する",
			fnErr:       ErrUpstreamUnauthorized,
			authMode:    AuthModeLocalHMAC,
			wantErr:     true,
			wantFnCalls: 1,
		},
		{
			// 実機検証で発覚した不具合の回帰テスト: admin画面でログイン中のユーザーを
			// 削除した場合、backend は ErrUserNotProvisioned を返す
			// リフレッシュしても対象ユーザーは戻らないため、リフレッシュを試みずセッションを破棄する
			name:        "ユーザーが存在しない場合はリフレッシュを試みずセッションを破棄する",
			fnErr:       ErrUserNotProvisioned,
			wantErr:     true,
			wantFnCalls: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := newTestStore(t)
			ctx := context.Background()
			sessionID, err := store.Create(ctx, Session{
				AuthMode:        tt.authMode,
				AccessToken:     "old-access-token",
				RefreshToken:    "old-refresh-token",
				RefreshTokenExp: time.Now().Add(time.Hour),
			})
			if err != nil {
				t.Fatalf("store.Create() error = %v", err)
			}

			refresher := &fakeTokenRefresher{err: tt.refreshErr}
			r := NewRefresher(store, refresher)

			fnCalls := 0
			// fnはリトライ1回目だけ常にfnErrを返し、2回目(リフレッシュ後の再試行)は成功する、
			// という現実的な挙動を模す
			doErr := r.Do(ctx, sessionID, func(accessToken string) error {
				fnCalls++
				if fnCalls == 1 {
					return tt.fnErr
				}
				return nil
			})

			if (doErr != nil) != tt.wantErr {
				t.Fatalf("Do() error = %v, wantErr = %v", doErr, tt.wantErr)
			}
			if tt.wantFnCalls != 0 && fnCalls != tt.wantFnCalls {
				t.Errorf("fn call count = %d, want %d", fnCalls, tt.wantFnCalls)
			}
			if tt.wantRefreshed && refresher.calls != 1 {
				t.Errorf("refresh call count = %d, want 1", refresher.calls)
			}

			userGone := errors.Is(tt.fnErr, ErrUserNotProvisioned)
			if tt.refreshErr != nil || (tt.authMode != "" && tt.authMode != AuthModeKeycloak) || userGone {
				if _, err := store.Get(ctx, sessionID); !errors.Is(err, ErrSessionNotFound) {
					t.Errorf("session should be deleted after refresh failure, Get() error = %v", err)
				}
			}
			if tt.authMode != "" && tt.authMode != AuthModeKeycloak && refresher.calls != 0 {
				t.Errorf("refresh call count = %d, want 0 (local auth session must not attempt refresh)", refresher.calls)
			}
			if userGone && refresher.calls != 0 {
				t.Errorf("refresh call count = %d, want 0 (user-not-provisioned must not attempt refresh)", refresher.calls)
			}
		})
	}
}
