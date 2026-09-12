// セキュリティ関連のHTTPレスポンスヘッダーをまとめて付与するミドルウェア
//
// 【3回目のe2e監査で判明】bffのレスポンスにCache-Control等の明示的なキャッシュ制御
// ヘッダーが一切無く、ログアウト後もブラウザのbfcache(back-forward cache)経由で直前の画面が一瞬表示される余地が理論上残っていた
//
// OWASP Session Management
// Cheat Sheetがセッション情報を含むレスポンスへの明示的なキャッシュ禁止を推奨していること、
// および主要ブラウザがCache-Control: no-storeをbfcache対象外の判定シグナルとして
// 扱っていることから、これに対応する
package auth

import "github.com/gin-gonic/gin"

// SecurityHeadersMiddleware はbffの全レスポンスに以下のヘッダーを付与する
//   - Cache-Control: no-store, Pragma: no-cache
//     bffはAPI専用でありレスポンスはユーザーごとに異なる/認証状態に依存するため、
//     静的アセットのような「キャッシュしてよいレスポンス」は元々存在しない
//     (frontendの静的アセットはVite側が別途配信する)
//     そのため個別のルートで出し分けず、bffの全レスポンスに一律で付与する判断にした
//   - X-Content-Type-Options: nosniff
//     ブラウザがContent-Typeを無視して中身を推測する(MIMEスニッフィング)ことによる誤解釈を防ぐ
//     bffはJSON以外を返さないため、常に付与して問題ない
//   - X-Frame-Options: DENY
//     bffはAPI専用+OIDCのリダイレクト系エンドポイントのみを持ち、
//     第三者サイトのiframeに埋め込まれる正当な理由が無いため、クリックジャッキング対策として
//     一律拒否する
func SecurityHeadersMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		c.Header("Pragma", "no-cache")
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("X-Frame-Options", "DENY")
		c.Next()
	}
}
