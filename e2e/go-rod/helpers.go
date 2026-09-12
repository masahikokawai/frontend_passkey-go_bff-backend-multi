package e2e_go_rod

import (
	"fmt"
	"os"
	"time"

	"github.com/go-rod/rod"
)

// baseURL はfrontend(Vite dev server)のベースURL(e2e/SELECTORS.md参照)
func baseURL() string {
	return getEnv("FRONTEND_BASE_URL", "http://localhost:5173")
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func testid(name string) string {
	return fmt.Sprintf(`[data-testid="%s"]`, name)
}

// setValueViaJS はReactの制御コンポーネント(controlled input)に対して、
// ネイティブのvalueセッターを直接呼んでからinput/changeイベントを発火させる
//
// 【e2e/selenium/test/helpers.jsと同じ理由での対応】<select>やdate inputは、
// go-rodのMustInput/MustSelectの挙動(内部でのキーストローク合成やテキスト一致)が
// Reactのコントロールドコンポーネントと相性が悪いケースがあるため、
// Selenium版で採用したのと同じ「ネイティブsetterを直接叩く」手法に統一する
// (task-name-input等の素直なtext/emailInputはMustInputで問題ない)
func setValueViaJS(el *rod.Element, value string) {
	el.MustEval(`function(val) {
		const proto = Object.getPrototypeOf(this);
		const desc = Object.getOwnPropertyDescriptor(proto, 'value');
		desc.set.call(this, val);
		this.dispatchEvent(new Event('input', { bubbles: true }));
		this.dispatchEvent(new Event('change', { bubbles: true }));
	}`, value)
}

// loginViaLocal はローカル認証(HMAC版・RSA版共通)のログインフォームからのログイン
// (CONTRACT.md セクション16.1: /login はHMAC版、/login/rsa はRSA版
// フォーム自体は同じコンポーネント(LoginForm.tsx)を共用している)
func loginViaLocal(page *rod.Page, rsa bool) {
	email := getEnv("E2E_LOCAL_EMAIL", "local-user@example.com")
	password := getEnv("E2E_LOCAL_PASSWORD", "password")

	if rsa {
		// /tasksへのnavigateで既に/loginへリダイレクトされている状態から、RSA版へのリンクで遷移する
		page.MustElement(testid("login-rsa-link")).MustWaitVisible().MustClick()
	}

	page.MustElement(testid("login-email-input")).MustWaitVisible().MustInput(email)
	page.MustElement(testid("login-password-input")).MustInput(password)
	page.MustElement(testid("login-submit-button")).MustClick()
	page.MustElement(testid("task-list")).MustWaitVisible()
}

// loginViaKeycloak はKeycloakでのログイン
// frontendの/loginは即座にKeycloakへ遷移しない(CONTRACT.md セクション16)ため、
// フォーム内の「Keycloakでログイン」ボタンを明示的にクリックする必要がある
func loginViaKeycloak(page *rod.Page) {
	username := getEnv("E2E_USERNAME", "general-user")
	password := getEnv("E2E_PASSWORD", "password")

	page.MustElement(testid("login-keycloak-button")).MustWaitVisible().MustClick()

	page.MustElement("#username").MustWaitVisible().MustInput(username)
	page.MustElement("#password").MustInput(password)
	page.MustElement("#kc-login").MustClick()
	page.MustElement(testid("task-list")).MustWaitVisible()
}

// waitLoginScreen はログイン画面(login-email-input)が表示されるまで待つ
// Keycloakのログアウトは複数回のリダイレクトを経る(playwright版auth.spec.tsのコメント参照)ため、
// 途中のURLではなく最終状態を十分な猶予時間で待つ
func waitLoginScreen(page *rod.Page, timeout time.Duration) {
	page.Timeout(timeout).MustElement(testid("login-email-input")).MustWaitVisible()
}

func uniqueTaskName(prefix string) string {
	// Task.nameは20文字以内のバリデーション(Rails/Go両方共通)のため短く保つ
	if prefix == "" {
		prefix = "E2E"
	}
	return fmt.Sprintf("%s%d", prefix, time.Now().UnixMilli()%100000)
}
