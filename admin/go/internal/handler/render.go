// render.go はhtml/templateでの描画を担う
//
// training-go/gin/internal/handler/render.go(参考実装)と同じ考え方: 各ページの
// テンプレートを先に文字列としてレンダリングし、それをlayout.htmlへ
// template.HTML型として埋め込む(html/templateの自動エスケープを経た安全な
// 文字列を二重エスケープしないための決まったパターン)
package handler

import (
	"bytes"
	"html/template"
	"net/http"

	"github.com/gin-gonic/gin"
)

var templates *template.Template

// LoadTemplates はweb/templates配下の全*.htmlを1つの名前空間へ読み込む
// 各ファイルは `{{define "pages/xxx"}}` のようにユニークな名前を持つため、
// 1つの*template.Templateにまとめても名前の衝突が起きない
func LoadTemplates(glob string) {
	templates = template.Must(template.New("").Funcs(template.FuncMap{
		"boolLabel": boolLabel,
	}).ParseGlob(glob))
}

type layoutData struct {
	Content template.HTML
	Error   string
	Flag    any
}

// render はpageName(例: "pages/index")のテンプレートを先にレンダリングし、
// その結果をlayout.htmlへ埋め込んで最終的なHTMLを書き出す
func render(c *gin.Context, status int, pageName string, data gin.H) {
	var body bytes.Buffer
	if err := templates.ExecuteTemplate(&body, pageName, data); err != nil {
		c.String(http.StatusInternalServerError, "template render error(%s): %v", pageName, err)
		return
	}

	errMsg, _ := data["Error"].(string)

	var out bytes.Buffer
	if err := templates.ExecuteTemplate(&out, "layout", layoutData{
		Content: template.HTML(body.String()), //nolint:gosec // 上でhtml/templateにより安全にレンダリング済みの文字列を埋め込むだけ
		Error:   errMsg,
	}); err != nil {
		c.String(http.StatusInternalServerError, "layout render error: %v", err)
		return
	}
	c.Data(status, "text/html; charset=utf-8", out.Bytes())
}
