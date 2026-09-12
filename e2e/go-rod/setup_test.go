package e2e_go_rod

import (
	"testing"

	"github.com/go-rod/rod"
)

// newPage はheadlessブラウザを起動し、テスト終了時に自動でクローズする
// go-rodはchromedpと違い、launcher.New()を明示せずrod.New().MustConnect()だけで
// システムにインストール済み(または自動ダウンロードした)Chromiumに接続できる
func newPage(t *testing.T) *rod.Page {
	t.Helper()
	browser := rod.New().MustConnect()
	t.Cleanup(func() { _ = browser.Close() })
	return browser.MustPage()
}

func gotoPath(page *rod.Page, path string) *rod.Page {
	page.MustNavigate(baseURL() + path).MustWaitLoad()
	return page
}
