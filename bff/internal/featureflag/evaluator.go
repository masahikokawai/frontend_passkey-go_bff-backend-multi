// Package featureflag はOpenFeature Go SDK + GO Feature Flagによるフラグ評価をラップする
//
// アプリコードが特定ベンダー(この場合GO Feature Flag)のAPIへ直接依存しないよう、Evaluatorという薄いinterfaceの背後に隠す
// 将来flagdやLaunchDarklyへ差し替える場合もこのファイルだけを差し替えれば済む
package featureflag

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

// Evaluator はフラグ評価の窓口
// auth.FlagEvaluator / proxy 側の interface はこのメソッドシグネチャに構造的に一致する
type Evaluator struct {
	client *openfeature.Client
	logger *slog.Logger
}

// 【CONTRACT.mdセクション13で確定した変更】
// 以前はファイル(flags.yaml)ベースの in-process provider だったが、
// Feature Flag の設定は MySQL(feature_flagsテーブル)を正本にし、admin/go・admin/rails から編集できるようにした
//
// bff は DB へ直接繋がず、backend が公開する GET /internal/v1/feature-flags/export を定期ポーリングする
// HTTP retrieverへ切り替える(bffはbackend経由でのみデータを扱うという既存のアーキテクチャ方針を維持するため)
//
// NewEvaluator は backend の feature-flags/export エンドポイントを HTTP retriever でポーリングし、
// OpenFeature のグローバルプロバイダとして GO Feature Flag(in-process)を登録する
func NewEvaluator(ctx context.Context, exportURL, pollToken string, logger *slog.Logger) (*Evaluator, error) {
	header := http.Header{}
	header.Set("X-Feature-Flag-Poll-Token", pollToken)

	provider, err := gofeatureflaginprocess.NewProviderWithContext(ctx, gofeatureflaginprocess.ProviderOptions{
		GOFeatureFlagConfig: &ffclient.Config{
			PollingInterval: 10 * time.Second,
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

	return &Evaluator{client: openfeature.NewClient("bff-gin"), logger: logger}, nil
}

// BoolValue はtargeting key(常にログイン中ユーザーのuser_id)を指定してbool値を評価する
// 評価エラー時はdefaultValueを返す(未定義キーへの防御でもある)
//
// 【切り替えの可視化】admin/go・admin/railsからのDB変更が実際にどちらの実装を
// 動かしたか追えるよう、評価結果を必ずINFOログに残す(bff.tasks-backend-v2 /
// frontend.tasks-ts-rewrite いずれの評価もこの1箇所を通るため、呼び出し側を
// 個別に変更しなくてもここだけで両フラグの切り替えが可視化できる)
func (e *Evaluator) BoolValue(ctx context.Context, flagKey string, defaultValue bool, userID string) bool {
	evalCtx := openfeature.NewEvaluationContext(userID, map[string]any{})
	value, err := e.client.BooleanValue(ctx, flagKey, defaultValue, evalCtx)
	if err != nil {
		if e.logger != nil {
			e.logger.Warn("feature flag評価に失敗、defaultValueを使用",
				slog.String("flag", flagKey), slog.String("user_id", userID),
				slog.Bool("default_value", defaultValue), slog.Any("error", err))
		}
		return defaultValue
	}
	if e.logger != nil {
		e.logger.Info("feature flag評価",
			slog.String("flag", flagKey), slog.String("user_id", userID), slog.Bool("value", value))
	}
	return value
}

// StringValue はBoolValueと同じ仕組みで、文字列3値以上の多値(multivariate)フラグを評価する
// CONTRACT.mdセクション19: frontend.task-create-ux(inline/modal/page)のような、
// 「排他的な複数の選択肢から1つを選ぶ」フラグはbooleanでは表現できないため新設した
func (e *Evaluator) StringValue(ctx context.Context, flagKey string, defaultValue string, userID string) string {
	evalCtx := openfeature.NewEvaluationContext(userID, map[string]any{})
	value, err := e.client.StringValue(ctx, flagKey, defaultValue, evalCtx)
	if err != nil {
		if e.logger != nil {
			e.logger.Warn("feature flag評価に失敗、defaultValueを使用",
				slog.String("flag", flagKey), slog.String("user_id", userID),
				slog.String("default_value", defaultValue), slog.Any("error", err))
		}
		return defaultValue
	}
	if e.logger != nil {
		e.logger.Info("feature flag評価",
			slog.String("flag", flagKey), slog.String("user_id", userID), slog.String("value", value))
	}
	return value
}
