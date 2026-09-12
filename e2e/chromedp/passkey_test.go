package e2e

import (
	"context"
	"testing"
	"time"

	"github.com/chromedp/cdproto/webauthn"
	"github.com/chromedp/chromedp"
)

// enableVirtualAuthenticator はCDPのWebAuthnドメインで仮想認証器を有効化する。
//
// 【なぜこの実装にしたか】パスキーのE2Eは、実機の生体認証(Face ID/指紋/PINダイアログ)が
// 絡むため、素朴にはUIオートメーションで自動化できないように見える。しかし
// Chrome DevTools Protocolには`WebAuthn`ドメインがあり、「仮想の認証器」をテストコードから
// 登録できる。`navigator.credentials.create()`/`.get()`は登録した仮想認証器を
// 実在の認証器と区別せず扱うため、frontend側のコードは一切変更せず、実際のパスキー
// registration/authenticationのフロー(challenge発行→署名→検証)をそのまま検証できる。
// - Protocol: ctap2(WebAuthn Level 2以降の実装が前提のため。u2fは古い規格でresident key非対応)
// - HasResidentKey: true(CONTRACT.mdセクション22.2の「discoverable credential」方式の検証に必須。
//   これが無いと`allowCredentials`指定無しのログイン時に認証器がcredentialを提示できない)
// - AutomaticPresenceSimulation: true(ユーザーの物理的なタッチ/クリックの代わりに、
//   即座に「本人確認OK」を返す。CI上で人手を介さず完走させるための設定)
// - IsUserVerified: true + HasUserVerification: true(bffのgo-webauthn設定はUser Verification
//   必須では無いが、実運用のパスキーは生体認証等でUVを伴うのが通例なので、より実態に近い
//   仮想認証器の設定にしている)
func enableVirtualAuthenticator(ctx context.Context) error {
	// 【実機検証で判明】webauthn.Enable()・webauthn.AddVirtualAuthenticator(...)の
	// .Do(ctx)を、chromedp.Run()を介さずこのctxへ直接呼ぶと"invalid context"になる。
	// chromedp.Runは呼ばれるたびに`cdp.WithExecutor(ctx, c.Target)`で実行用のexecutorを
	// 積んだ「その場限りの派生ctx」をActionへ渡す実装になっており(chromedp.go の
	// `func Run`参照)、そのexecutorは呼び出し元が保持するctx変数へは残らない。
	// そのため、Actionは必ずchromedp.Run(ctx, ...)を経由して実行する必要がある。
	return chromedp.Run(ctx,
		webauthn.Enable(),
		chromedp.ActionFunc(func(ctx context.Context) error {
			_, err := webauthn.AddVirtualAuthenticator(&webauthn.VirtualAuthenticatorOptions{
				Protocol:                    webauthn.AuthenticatorProtocolCtap2,
				Transport:                   webauthn.AuthenticatorTransportInternal,
				HasResidentKey:              true,
				HasUserVerification:         true,
				IsUserVerified:              true,
				AutomaticPresenceSimulation: true,
			}).Do(ctx)
			return err
		}),
	)
}

// TestPasskeyRegisterAndLogin は
// 「ローカル認証(HMAC)でログイン→/accountでパスキー登録→ログアウト→パスキーでログイン」を検証する。
// CONTRACT.mdセクション22.1により、パスキーの対象はbffのローカル認証ユーザーのみ
// (Keycloak発行ユーザーは対象外)なので、ログインはloginViaLocal(HMAC)を使う。
func TestPasskeyRegisterAndLogin(t *testing.T) {
	ctx, cancel := context.WithTimeout(newBrowserContext(t), 30*time.Second)
	defer cancel()

	// 仮想認証器の有効化は、navigator.credentials.*を呼ぶより前であればどのタイミングでもよいが、
	// 分かりやすさのためNavigateの直後・ログイン前に行う
	if err := chromedp.Run(ctx, chromedp.Navigate(frontendBaseURL()+"/tasks")); err != nil {
		t.Fatalf("Navigate() error = %v", err)
	}
	if err := enableVirtualAuthenticator(ctx); err != nil {
		t.Fatalf("enableVirtualAuthenticator() error = %v", err)
	}
	if err := loginViaLocal(ctx, false); err != nil {
		t.Fatalf("loginViaLocal(HMAC) error = %v", err)
	}

	// パスキー登録: /account画面の「パスキーを登録」ボタン
	err := chromedp.Run(ctx,
		chromedp.Navigate(frontendBaseURL()+"/account"),
		chromedp.WaitVisible(`[data-testid="passkey-register-button"]`, chromedp.ByQuery),
		chromedp.Click(`[data-testid="passkey-register-button"]`, chromedp.ByQuery),
		chromedp.WaitVisible(`[data-testid="passkey-register-success"]`, chromedp.ByQuery),
	)
	if err != nil {
		t.Fatalf("パスキー登録に失敗: %v", err)
	}

	// 一度ログアウトし、パスキーのみ(Keycloak・パスワード不要)でログインできることを検証する
	err = chromedp.Run(ctx,
		chromedp.Click(`[data-testid="logout-button"]`, chromedp.ByQuery),
		chromedp.WaitVisible(`[data-testid="login-email-input"]`, chromedp.ByQuery),
	)
	if err != nil {
		t.Fatalf("ログアウトに失敗: %v", err)
	}

	loginCtx, cancelLogin := context.WithTimeout(ctx, 15*time.Second)
	defer cancelLogin()
	err = chromedp.Run(loginCtx,
		chromedp.WaitVisible(`[data-testid="login-passkey-button"]`, chromedp.ByQuery),
		chromedp.Click(`[data-testid="login-passkey-button"]`, chromedp.ByQuery),
		chromedp.WaitVisible(`[data-testid="task-list"]`, chromedp.ByQuery),
	)
	if err != nil {
		t.Fatalf("パスキーでのログインに失敗: %v", err)
	}
}
