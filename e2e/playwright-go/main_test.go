// Package e2e はplaywright-goによるE2Eテスト(CONTRACT.mdセクション18)
// JS版(../playwright/)と全く同じシナリオを、Go標準のtestingパッケージで実装している
package e2e

import (
	"os"
	"testing"

	"github.com/mxschmitt/playwright-go"
)

// pw/browserはプロセス全体で1つだけ起動し(起動コストが高いため)、
// テストごとにNewContextで新しいブラウザコンテキスト(Cookie等が独立)を作る
// JS版のPlaywright Test(@playwright/test)がテストごとに自動でやっていることを、ここではTestMain+newPageヘルパーで手動再現している
var (
	pw      *playwright.Playwright
	browser playwright.Browser
	baseURL string
)

func TestMain(m *testing.M) {
	var err error
	pw, err = playwright.Run()
	if err != nil {
		panic("playwrightドライバの起動に失敗しました(`go run github.com/mxschmitt/playwright-go/cmd/playwright install --with-deps`を実行済みか確認): " + err.Error())
	}

	browser, err = pw.Chromium.Launch(playwright.BrowserTypeLaunchOptions{
		Headless: playwright.Bool(os.Getenv("E2E_HEADED") == ""),
	})
	if err != nil {
		panic("Chromiumの起動に失敗しました: " + err.Error())
	}

	baseURL = envOr("FRONTEND_BASE_URL", "http://localhost:5173")

	code := m.Run()

	_ = browser.Close()
	_ = pw.Stop()
	os.Exit(code)
}

// newPage はテストごとに独立したブラウザコンテキスト+ページを作る
// t.Cleanupでテスト終了時に自動的にコンテキストを閉じる(Cookie等の状態が
// テスト間で漏れないようにするため
// JS版のPlaywright Testの既定動作と同じ)
func newPage(t *testing.T) playwright.Page {
	t.Helper()
	ctx, err := browser.NewContext(playwright.BrowserNewContextOptions{
		BaseURL: playwright.String(baseURL),
	})
	if err != nil {
		t.Fatalf("ブラウザコンテキストの作成に失敗しました: %v", err)
	}
	t.Cleanup(func() { _ = ctx.Close() })

	page, err := ctx.NewPage()
	if err != nil {
		t.Fatalf("ページの作成に失敗しました: %v", err)
	}
	return page
}
