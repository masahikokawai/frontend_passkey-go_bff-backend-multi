package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	gofeatureflaginprocess "github.com/open-feature/go-sdk-contrib/providers/go-feature-flag-in-process/pkg"
	"github.com/open-feature/go-sdk/openfeature"
	ffclient "github.com/thomaspoignant/go-feature-flag"
	"github.com/thomaspoignant/go-feature-flag/retriever/httpretriever"
)

// languageTargetingKey はbackend.task-languageを評価する際のtargeting key
// bffは常にログイン中ユーザーのuser_idを使うが、外部公開APIはサーバー間連携で
// ユーザーセッションという概念が無いため、固定値を使う(このフラグにユーザー単位の
// targeting ruleは設定されていないため評価結果に影響しない)
const languageTargetingKey = "gateway"

// LanguageResolver はbackend.task-languageを評価する窓口
// bff/internal/featureflag/evaluator.goと全く同じ仕組み(OpenFeature Go SDK +
// GO Feature Flag、backendのexportエンドポイントをHTTP retrieverでポーリング)を、
// このゲートウェイ専用に再構築したもの(モジュールが別なのでbffのinternalパッケージは
// importできないため、同じライブラリ構成を薄くラップし直している)
type LanguageResolver struct {
	client *openfeature.Client
	logger *slog.Logger
}

// NewLanguageResolver はexportURLをポーリングするproviderを起動する
func NewLanguageResolver(ctx context.Context, exportURL, pollToken string, pollInterval time.Duration, logger *slog.Logger) (*LanguageResolver, error) {
	header := http.Header{}
	header.Set("X-Feature-Flag-Poll-Token", pollToken)

	provider, err := gofeatureflaginprocess.NewProviderWithContext(ctx, gofeatureflaginprocess.ProviderOptions{
		GOFeatureFlagConfig: &ffclient.Config{
			PollingInterval: pollInterval,
			FileFormat:      "json",
			Retriever: &httpretriever.Retriever{
				URL:     exportURL,
				Method:  http.MethodGet,
				Header:  header,
				Timeout: 5 * time.Second,
			},
			Context: ctx,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("GO Feature Flag providerの初期化に失敗しました(url=%s): %w", exportURL, err)
	}

	if err := openfeature.SetProviderAndWait(provider); err != nil {
		return nil, fmt.Errorf("OpenFeature providerの登録に失敗しました: %w", err)
	}

	return &LanguageResolver{client: openfeature.NewClient("gateway-go"), logger: logger}, nil
}

// Resolve は backend.task-language の現在値を返す
// 評価に失敗した場合は "go" を既定値にする
func (r *LanguageResolver) Resolve(ctx context.Context) string {
	evalCtx := openfeature.NewEvaluationContext(languageTargetingKey, map[string]any{})
	value, err := r.client.StringValue(ctx, "backend.task-language", "go", evalCtx)
	if err != nil {
		if r.logger != nil {
			r.logger.Warn("backend.task-language評価に失敗、既定値(go)を使用", slog.Any("error", err))
		}
		return "go"
	}
	return value
}
