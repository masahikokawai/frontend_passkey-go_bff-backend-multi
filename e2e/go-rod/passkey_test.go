package e2e_go_rod

import (
	"os"
	"testing"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/proto"
)

// enableVirtualAuthenticator はCDPのWebAuthnドメインで仮想認証器を有効化する
//
// 【なぜこの実装にしたか】
// e2e/chromedp/passkey_test.goのenableVirtualAuthenticatorと同じ考え方
// (このプロジェクトのCDP系3フレームワーク(chromedp/go-rod/playwright-go)で揃えている)
//
// go-rod は CDP のドメインをラップしていないコマンドも`proto.WebAuthnXxx{...}.Call(page)`という形でそのまま叩けるため、
// chromedp版と実質同じ設定を移植するだけで済む
func enableVirtualAuthenticator(page *rod.Page) error {
	if err := (proto.WebAuthnEnable{}).Call(page); err != nil {
		return err
	}
	_, err := (proto.WebAuthnAddVirtualAuthenticator{
		Options: &proto.WebAuthnVirtualAuthenticatorOptions{
			Protocol:                    proto.WebAuthnAuthenticatorProtocolCtap2,
			Transport:                   proto.WebAuthnAuthenticatorTransportInternal,
			HasResidentKey:              true,
			HasUserVerification:         true,
			IsUserVerified:              true,
			AutomaticPresenceSimulation: true,
		},
	}).Call(page)
	return err
}

// newPageWithSystemChrome はこのシナリオ専用に、go-rodが自動ダウンロードするChromiumではなく
// システムにインストール済みのGoogle Chromeを使ってページを起動する
//
// 【実機検証で発覚した問題・なぜこの対応が必要か】
// go-rod の既定動作(`rod.New().MustConnect()`、
// setup_test.goのnewPage参照)は、go-rodが自動ダウンロードする特定リビジョンのChromium
// (実機確認時点でchromium-1321438、`Chromium 128.0.6568.0`を自称)に接続する
//
// このビルドは`PublicKeyCredential.parseCreationOptionsFromJSON`/
// `parseRequestOptionsFromJSON`(frontend/src/shared/webauthn/passkey.tsが機能検出に使っている
// WebAuthn Level 3のJSON直列化API)を持っておらず、frontend側が「このブラウザはパスキーに
// 対応していません」という`PasskeyUnsupportedError`を投げてしまい、E2Eが恒久的にタイムアウトする
// (originally: `passkey-register-success`要素を待ち続けて`MustWaitVisible`がハングし、
// 実際には`passkey-register-error`が表示されていた
//
// `document.body.innerHTML`を直接評価する簡易デバッグで判明した
//
// これはgo-rod自体のバグではなく、「go-rodが同梱するオープンソースのChromiumスナップショットが、
// Google Chrome(ブランド版)より一部の新しいWeb Platform APIの有効化が遅れることがある」という、ブラウザ配布形態の違いに起因する既知の制約
// CDP自体(WebAuthnドメイン)は同じ128系でも
// 正しく動く(仮想認証器の登録自体は成功する)ため、あくまでfrontend側が要求するJS APIの対応状況の差でしかない
//
// この対応: 環境変数`CHROME_BIN`(未設定時はmacOSの既定インストール先)で指定した、実際にインストール
// 済みのGoogle Chromeバイナリを使って起動することで回避する
// 他の既存シナリオ(setup_test.go の newPage)には影響を与えないよう、このファイル内だけのヘルパーとして局所的に用意した
func newPageWithSystemChrome(t *testing.T) *rod.Page {
	t.Helper()
	chromeBin := os.Getenv("CHROME_BIN")
	if chromeBin == "" {
		chromeBin = "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"
	}
	if _, err := os.Stat(chromeBin); err != nil {
		t.Skipf("システムのGoogle Chromeが見つからないためスキップします(CHROME_BIN未設定・パス=%s): %v", chromeBin, err)
	}

	l := launcher.New().Bin(chromeBin).Headless(true)
	controlURL, err := l.Launch()
	if err != nil {
		t.Fatalf("Google Chromeの起動に失敗しました: %v", err)
	}
	// 【テスト監査で発見・修正】l.Kill()の登録をbrowser生成後(旧: 1つのt.Cleanupにまとめていた)
	// まで遅らせると、直後のMustConnect()がpanicした場合(rod.New().ControlURL(...).MustConnect()は
	// go-rodの流儀でエラーをpanicとして返す)、既に起動済みのこのChromeプロセスを
	// 終了させる手段が一切登録されないままテストが終わり、プロセスがリークする。
	// l.Launch()成功直後、MustConnect()より前にt.Cleanupを分けて登録することで、
	// 以降どこでpanicしてもこのプロセスだけは必ず終了できるようにする。
	t.Cleanup(func() { l.Kill() })
	browser := rod.New().ControlURL(controlURL).MustConnect()
	t.Cleanup(func() { _ = browser.Close() })
	return browser.MustPage()
}

// TestPasskeyRegisterAndLogin は
// 「ローカル認証(HMAC)でログイン→/accountでパスキー登録→ログアウト→パスキーでログイン」を検証する
// CONTRACT.mdセクション22.1により対象はbffのローカル認証ユーザーのみ(Keycloak発行ユーザーは対象外)
func TestPasskeyRegisterAndLogin(t *testing.T) {
	page := newPageWithSystemChrome(t)
	gotoPath(page, "/tasks")
	if err := enableVirtualAuthenticator(page); err != nil {
		t.Fatalf("enableVirtualAuthenticator() error = %v", err)
	}
	loginViaLocal(page, false)

	// パスキー登録: /account画面の「パスキーを登録」ボタン
	gotoPath(page, "/account")
	page.MustElement(testid("passkey-register-button")).MustWaitVisible().MustClick()
	if !page.MustElement(testid("passkey-register-success")).MustWaitVisible().MustVisible() {
		t.Fatal("passkey-register-success が表示されていない(パスキー登録に失敗)")
	}

	// 一度ログアウトし、パスキーのみ(Keycloak・パスワード不要)でログインできることを検証する
	page.MustElement(testid("logout-button")).MustClick()
	waitLoginScreen(page, 15*time.Second)

	page.MustElement(testid("login-passkey-button")).MustWaitVisible().MustClick()
	if !page.MustElement(testid("task-list")).MustWaitVisible().MustVisible() {
		t.Fatal("パスキーでのログイン後にtask-listが表示されていない")
	}
}
