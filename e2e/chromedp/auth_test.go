package e2e

import (
	"context"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// TestUnauthenticatedRedirectsToLocalLogin は
// e2e/playwright/tests/auth.spec.ts の同名シナリオ(1つ目)に対応する。
func TestUnauthenticatedRedirectsToLocalLogin(t *testing.T) {
	ctx, cancel := context.WithTimeout(newBrowserContext(t), 30*time.Second)
	defer cancel()

	// /tasksは保護ルート。useAuthが叩く/api/meの401をapiFetchが検知し、
	// /login?redirect=... へ遷移する(Keycloakへの自動遷移はしない。CONTRACT.md セクション16)
	err := chromedp.Run(ctx,
		chromedp.Navigate(frontendBaseURL()+"/tasks"),
		chromedp.WaitVisible(`[data-testid="login-email-input"]`, chromedp.ByQuery),
	)
	if err != nil {
		t.Fatalf("未ログイン状態でのリダイレクトに失敗: %v", err)
	}
}

// TestLocalHMACLoginAndLogout は
// 「ローカル認証(HMAC版・既定)でログイン→タスク一覧表示→ログアウト→ログイン画面に戻る」を検証する。
func TestLocalHMACLoginAndLogout(t *testing.T) {
	ctx, cancel := context.WithTimeout(newBrowserContext(t), 30*time.Second)
	defer cancel()

	if err := chromedp.Run(ctx, chromedp.Navigate(frontendBaseURL()+"/tasks")); err != nil {
		t.Fatalf("Navigate() error = %v", err)
	}
	if err := loginViaLocal(ctx, false); err != nil {
		t.Fatalf("loginViaLocal(HMAC) error = %v", err)
	}

	// ローカルセッションのログアウトはKeycloakのRP-Initiated Logoutを経由せず、
	// bffがFrontendBaseURLへの相対パスをそのまま返す(CONTRACT.md セクション16.4)
	// window.location.hrefでの遷移後、未ログイン状態の/がuseAuthの401検知で
	// 再度/loginへリダイレクトされる、という2段の自動遷移になる。
	logoutCtx, cancelLogout := context.WithTimeout(ctx, 15*time.Second)
	defer cancelLogout()
	err := chromedp.Run(logoutCtx,
		chromedp.Click(`[data-testid="logout-button"]`, chromedp.ByQuery),
		chromedp.WaitVisible(`[data-testid="login-email-input"]`, chromedp.ByQuery),
	)
	if err != nil {
		t.Fatalf("ログアウト後にログイン画面へ戻らなかった: %v", err)
	}
}

// TestLocalRSALogin は「ローカル認証(RSA版)でログイン→タスク一覧表示」を検証する。
func TestLocalRSALogin(t *testing.T) {
	ctx, cancel := context.WithTimeout(newBrowserContext(t), 30*time.Second)
	defer cancel()

	if err := chromedp.Run(ctx, chromedp.Navigate(frontendBaseURL()+"/tasks")); err != nil {
		t.Fatalf("Navigate() error = %v", err)
	}
	if err := loginViaLocal(ctx, true); err != nil {
		t.Fatalf("loginViaLocal(RSA) error = %v", err)
	}
}

// TestLocalLoginWrongPassword は誤ったパスワードでのログイン失敗を検証する。
func TestLocalLoginWrongPassword(t *testing.T) {
	ctx, cancel := context.WithTimeout(newBrowserContext(t), 30*time.Second)
	defer cancel()

	err := chromedp.Run(ctx,
		chromedp.Navigate(frontendBaseURL()+"/login"),
		chromedp.WaitVisible(`[data-testid="login-email-input"]`, chromedp.ByQuery),
		chromedp.SendKeys(`[data-testid="login-email-input"]`, "local-user@example.com", chromedp.ByQuery),
		chromedp.SendKeys(`[data-testid="login-password-input"]`, "wrong-password", chromedp.ByQuery),
		chromedp.Click(`[data-testid="login-submit-button"]`, chromedp.ByQuery),
		chromedp.WaitVisible(`[data-testid="login-error"]`, chromedp.ByQuery),
		chromedp.WaitVisible(`[data-testid="login-email-input"]`, chromedp.ByQuery),
	)
	if err != nil {
		t.Fatalf("誤ったパスワードでのエラー表示に失敗: %v", err)
	}
}

// TestKeycloakLoginAndLogout は
// 「Keycloakでログイン→タスク一覧表示→ログアウト→ログイン画面に戻る」を検証する。
func TestKeycloakLoginAndLogout(t *testing.T) {
	ctx, cancel := context.WithTimeout(newBrowserContext(t), 30*time.Second)
	defer cancel()

	if err := chromedp.Run(ctx, chromedp.Navigate(frontendBaseURL()+"/tasks")); err != nil {
		t.Fatalf("Navigate() error = %v", err)
	}
	if err := loginViaKeycloak(ctx); err != nil {
		t.Fatalf("loginViaKeycloak() error = %v", err)
	}

	// ログアウト後の遷移は複数段ある: bffの/api/auth/logout(fetch)
	// → window.location.hrefでKeycloakのend_session_endpointへ実遷移(SSOセッション終了)
	// → post_logout_redirect_uriで frontendのトップへ戻る
	// → ProtectedLayoutが未ログインを検知し /login へ自動的にリダイレクトされる
	// 【playwright版と同じ理由で、中間URLは待たず最終状態だけを十分な猶予時間で待つ】
	// 途中のURLをwaitForで待つと、その後さらに自動リダイレクトが続いている最中に
	// 次のアクションが割り込んでしまい、ログイン画面に正しくたどり着けないことがある。
	logoutCtx, cancelLogout := context.WithTimeout(ctx, 15*time.Second)
	defer cancelLogout()
	err := chromedp.Run(logoutCtx,
		chromedp.Click(`[data-testid="logout-button"]`, chromedp.ByQuery),
		chromedp.WaitVisible(`[data-testid="login-email-input"]`, chromedp.ByQuery),
	)
	if err != nil {
		t.Fatalf("Keycloakログアウト後にログイン画面へ戻らなかった: %v", err)
	}
}
