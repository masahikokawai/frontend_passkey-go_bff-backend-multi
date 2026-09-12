// リクエストボディの上限サイズを制限するミドルウェア
//
// 【テスト監査で判明】bffのJSON受け取りエンドポイント(Task/Label CRUD・ローカル認証・
// パスキー)には、リクエストボディのサイズ上限が一切設定されていなかった。GinのShouldBindJSON
// は内部でjson.Decoderをボディへそのまま向けるだけで、標準ライブラリのjson.Decoder自体には
// サイズ上限が無い。そのため理論上、巨大なボディ(数百MB〜)を送りつけられると、
// 読み切るまでメモリを消費し続ける(DoSの一種)
//
// http.MaxBytesReaderでラップし、上限を超えた時点でread自体がエラーになるようにする
// (Goの標準的な対策方法。net/httpのServer.MaxHeaderBytesはヘッダーのみが対象でボディには効かない)
package auth

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// maxRequestBodyBytes はこのプロジェクトのTask/Label(説明文を含む)・ローカル認証・
// パスキー(attestation/assertion、公開鍵のCOSE形式バイト列を含む)のいずれも、
// 実際のペイロードは数KB程度に収まる想定のため、余裕を持たせつつ明らかに異常な
// 巨大リクエストは弾けるよう1MiBとする
const maxRequestBodyBytes = 1 << 20 // 1MiB

// MaxBodySizeMiddleware は全リクエストボディに上限サイズを適用する
// (http.MaxBytesReaderは既にio.ReadCloserを返すため、追加のラッパー型は不要)
func MaxBodySizeMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxRequestBodyBytes)
		c.Next()
	}
}
