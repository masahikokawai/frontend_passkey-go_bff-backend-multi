package main

import (
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
)

// defaultLanguage は backend.task-language が未実装の言語を指している場合のフォールバック先
// bffのTaskRoutes.pickClientと同じ考え方(CONTRACT.mdセクション20.7)
const defaultLanguage = "go"

// Gateway はbackend.task-languageの評価結果に応じて、外部公開APIリクエストを
// 対応する言語のbackendへリバースプロキシする
//
// 5言語すべてが外部公開API(/external/v1/tasks)を実装済み(CONTRACT.mdセクション11・20.7)
// resolveTargetのフォールバック経路は、将来flagに未知の値(実装ミスや手動でのDB編集ミス等)が
// 入った場合の安全策として残している
type Gateway struct {
	// Targets は 言語名 -> ベースURL のマップ(5言語ぶん)
	Targets map[string]string
	// ResolveLanguage は backend.task-language の現在値を返す関数
	ResolveLanguage func() string
	Logger          *slog.Logger
	// AllowedOrigin はConfig.AllowedOriginと同じ(swagger-uiのCORS許可オリジン)
	// 空文字列の場合はCORSヘッダを一切付与しない(既存のサーバー間クライアントのみを
	// 想定していたテスト構築コードとの後方互換のため、ゼロ値でも壊れないようにしている)
	AllowedOrigin string
}

// resolveTarget は要求された言語からルーティング先を決定する
// 戻り値のresolvedLanguageは実際に使われた言語(フォールバックした場合はdefaultLanguage)
func (g *Gateway) resolveTarget(requestedLanguage string) (resolvedLanguage, baseURL string, fellBack bool) {
	if u, ok := g.Targets[requestedLanguage]; ok {
		return requestedLanguage, u, false
	}
	return defaultLanguage, g.Targets[defaultLanguage], requestedLanguage != defaultLanguage
}

// Handler はCORS対応でラップした http.Handler を返す
// (内部はhttputil.ReverseProxy。戻り値の型をインターフェースにしても、
// httptest.NewServer等の既存の呼び出し側には影響しない)
//
// swagger-uiの「Try it out」機能だけがブラウザから直接このゲートウェイへcrossOriginで
// アクセスするため、実リクエストにAccess-Control-Allow-Originを付与し、
// プリフライト(OPTIONS)には上流へ転送せずこの層で直接応答する
// (上流のbackend外部APIはOPTIONSメソッドのルートを持たないため、転送すると
// 404/405になりプリフライト自体が失敗し、curlでの動作確認では気づけないまま
// 実際のブラウザでだけ壊れる、という発見しづらい壊れ方をする)
func (g *Gateway) Handler() http.Handler {
	return g.withCORS(g.reverseProxy())
}

func (g *Gateway) withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if g.AllowedOrigin != "" && origin == g.AllowedOrigin {
			w.Header().Set("Access-Control-Allow-Origin", g.AllowedOrigin)
			w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// reverseProxy は httputil.ReverseProxy を構築する
// Go 1.20+で追加されたRewrite(Directorの後継)を使い、リクエストごとに
// backend.task-language を評価してから転送先を決める
func (g *Gateway) reverseProxy() *httputil.ReverseProxy {
	return &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			requested := g.ResolveLanguage()
			resolved, base, fellBack := g.resolveTarget(requested)
			if fellBack && g.Logger != nil {
				g.Logger.Warn("backend.task-languageが未実装の言語を指しているためgoへフォールバック",
					slog.String("requested_language", requested), slog.String("fallback_language", resolved))
			}
			if g.Logger != nil {
				g.Logger.Info("外部公開APIリクエストを振り分け",
					slog.String("requested_language", requested), slog.String("resolved_language", resolved),
					slog.String("path", pr.In.URL.Path))
			}
			target, err := url.Parse(base)
			if err != nil {
				// 起動時に検証済みの URL のみを Targets へ入れる前提のため、ここに来るのはプログラミングミスの場合のみ
				// 空のURLへのリクエストが送られ502になる
				if g.Logger != nil {
					g.Logger.Error("転送先URLのパースに失敗しました", slog.String("base", base), slog.Any("error", err))
				}
				return
			}
			pr.SetURL(target)
			pr.SetXForwarded()
			// Client Credentials Grantのトークン(Authorizationヘッダ)はそのまま透過する
			// 認証検証はこのゲートウェイでは行わず、応答する backend 側の RequireExternalClientAuth に委譲する
		},
	}
}
