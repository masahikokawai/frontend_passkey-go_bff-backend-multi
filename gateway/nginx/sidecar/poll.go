// Package main はnginx製ゲートウェイのサイドカー
//
// nginx自体はbackend.task-languageのようなDBバックエンドのFeature Flagを評価できないため、
// このサイドカーが定期的にbackendのexportエンドポイントをポーリングし、値が変わったら
// upstream.confを書き換えて`nginx -s reload`をトリガーする(CONTRACT.mdセクション20.8)
//
// 【設計判断】gateway/go(アプリケーションコード内でリクエストごとに評価する)と違い、
// このサイドカーは「一定間隔で現在値を1つ取得し、設定ファイルに反映するだけ」でよいため、
// OpenFeature SDK + GO Feature Flagのフルスタックは使わず、exportエンドポイントのJSONを
// 直接パースする素朴な実装にしている(nginx+サイドカー方式の性質上、per-requestの評価は
// そもそも不要なため、この簡略化は妥当と判断した)
package main

import (
	"encoding/json"
	"fmt"
	"net/url"
)

// defaultLanguage は backend.task-language が未実装の言語を指している場合のフォールバック先
// gateway/goのGateway.resolveTargetと同じ考え方
const defaultLanguage = "go"

// flagVariant はbackendのexportエンドポイントが返すJSON(BuildFlagConfigJSON)の
// 1フラグぶんの形状のうち、このサイドカーが必要とする部分だけを抜き出したもの
type flagVariant struct {
	DefaultRule struct {
		Variation string `json:"variation"`
	} `json:"defaultRule"`
}

// parseLanguage はexportエンドポイントのレスポンスからbackend.task-languageの
// 現在値(default_variation)を取り出す
func parseLanguage(body []byte) (string, error) {
	var parsed map[string]flagVariant
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", fmt.Errorf("exportレスポンスのJSONパースに失敗しました: %w", err)
	}
	v, ok := parsed["backend.task-language"]
	if !ok {
		return "", fmt.Errorf("exportレスポンスにbackend.task-languageフラグが含まれていません")
	}
	if v.DefaultRule.Variation == "" {
		return "", fmt.Errorf("backend.task-languageのdefaultRule.variationが空です")
	}
	return v.DefaultRule.Variation, nil
}

// resolveTarget は要求された言語からルーティング先を決定する
// gateway/go(Go製ゲートウェイ)のGateway.resolveTargetと全く同じロジック
// 5言語すべてが外部公開APIを実装済みのため、フォールバックは未知の値が来た場合の安全策
func resolveTarget(requested string, targets map[string]string) (resolved, baseURL string, fellBack bool) {
	if u, ok := targets[requested]; ok {
		return requested, u, false
	}
	return defaultLanguage, targets[defaultLanguage], requested != defaultLanguage
}

// renderUpstreamConf はbaseURL(例: "http://127.0.0.1:8097")から、
// nginxの`upstream`ブロック定義(upstream.confの中身)を組み立てる
func renderUpstreamConf(baseURL string) (string, error) {
	u, err := url.Parse(baseURL)
	if err != nil {
		return "", fmt.Errorf("baseURLのパースに失敗しました(%s): %w", baseURL, err)
	}
	if u.Host == "" {
		return "", fmt.Errorf("baseURLにホストが含まれていません: %s", baseURL)
	}
	return fmt.Sprintf(
		"# このファイルはsidecarが自動的に書き換える(CONTRACT.mdセクション20.8)\n"+
			"# 手動編集してもsidecarの次回ポーリングで上書きされる\n"+
			"upstream backend_external {\n    server %s;\n}\n",
		u.Host,
	), nil
}
