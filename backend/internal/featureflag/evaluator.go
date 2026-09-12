// Package featureflag はOpenFeature Go SDK + GO Feature Flag(ファイルベース、
// in-processプロバイダ)によるFeature Flag評価をラップする
//
// bff/internal/featureflag と同じライブラリ構成にしているが、backend自身が
// 評価する理由はCONTRACT.mdセクション11参照(外部API /external/v1/tasks には
// BFFが介在しないため、ルーティング判断をbackend自身が持つ必要がある)
package featureflag

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	gofeatureflaginprocess "github.com/open-feature/go-sdk-contrib/providers/go-feature-flag-in-process/pkg"
	"github.com/open-feature/go-sdk/openfeature"
	ffclient "github.com/thomaspoignant/go-feature-flag"
	"github.com/thomaspoignant/go-feature-flag/retriever/fileretriever"

	"github.com/masahikokawai/training-go/bff-gin/backend/internal/repository"
)

// Evaluator はフラグ評価の窓口
type Evaluator struct {
	client *openfeature.Client
	logger *slog.Logger
}

// NewEvaluator はflags.yamlを読み込み、OpenFeatureのグローバルプロバイダとして
// GO Feature Flag(in-process)を登録する
//
// 【重要・CONTRACT.mdセクション13で置き換え済み】
// この関数はファイルベースだった頃の実装で、現在main.goからは呼ばれておらず、flags.yamlも実際には読まれない
// (「以前の設定方式の参考」として残しているだけ)
// 実際に使われているのは下の NewMySQLEvaluator(MySQLのfeature_flagsテーブルを正本にする)
func NewEvaluator(ctx context.Context, flagFilePath string) (*Evaluator, error) {
	provider, err := gofeatureflaginprocess.NewProviderWithContext(ctx, gofeatureflaginprocess.ProviderOptions{
		GOFeatureFlagConfig: &ffclient.Config{
			PollingInterval: 0,
			Retriever:       &fileretriever.Retriever{Path: flagFilePath},
			Context:         ctx,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("GO Feature Flag providerの初期化に失敗しました(path=%s): %w", flagFilePath, err)
	}

	if err := openfeature.SetProviderAndWait(provider); err != nil {
		return nil, fmt.Errorf("OpenFeature providerの登録に失敗しました: %w", err)
	}

	return &Evaluator{client: openfeature.NewClient("backend")}, nil
}

// BoolValue はtargeting key(外部APIではAPIクライアントのclient_id=azpクレーム)を指定してbool値を評価する
// 評価エラー時はdefaultValueを返す
//
// 【切り替えの可視化】admin/go・admin/railsからのDB変更が実際にどちらの実装を
// 動かしたか追えるよう、評価結果を必ずINFOログに残す
func (e *Evaluator) BoolValue(ctx context.Context, flagKey string, defaultValue bool, targetingKey string) bool {
	evalCtx := openfeature.NewEvaluationContext(targetingKey, map[string]any{})
	value, err := e.client.BooleanValue(ctx, flagKey, defaultValue, evalCtx)
	if err != nil {
		if e.logger != nil {
			e.logger.Warn("feature flag評価に失敗、defaultValueを使用",
				slog.String("flag", flagKey), slog.String("targeting_key", targetingKey),
				slog.Bool("default_value", defaultValue), slog.Any("error", err))
		}
		return defaultValue
	}
	if e.logger != nil {
		e.logger.Info("feature flag評価",
			slog.String("flag", flagKey), slog.String("targeting_key", targetingKey), slog.Bool("value", value))
	}
	return value
}

// StringValue はBoolValueと同じ仕組みで、文字列2値以上の多値(multivariate)フラグを評価する
//
// bff/internal/featureflag.Evaluator.StringValue と同じ考え方(CONTRACT.mdセクション19で
// frontend.task-create-uxのために新設されたもの)を踏襲した
// CONTRACT.mdセクション24: backend.external-tasks-orm(gorm/bob)の評価にこれを使う
// (bff側と違いtargeting keyはAPIクライアントのclient_id、外部APIにBFFが介在しないため)
func (e *Evaluator) StringValue(ctx context.Context, flagKey string, defaultValue string, targetingKey string) string {
	evalCtx := openfeature.NewEvaluationContext(targetingKey, map[string]any{})
	value, err := e.client.StringValue(ctx, flagKey, defaultValue, evalCtx)
	if err != nil {
		if e.logger != nil {
			e.logger.Warn("feature flag評価に失敗、defaultValueを使用",
				slog.String("flag", flagKey), slog.String("targeting_key", targetingKey),
				slog.String("default_value", defaultValue), slog.Any("error", err))
		}
		return defaultValue
	}
	if e.logger != nil {
		e.logger.Info("feature flag評価",
			slog.String("flag", flagKey), slog.String("targeting_key", targetingKey), slog.String("value", value))
	}
	return value
}

// NewMySQLEvaluator はfeature_flagsテーブルを正本にしてOpenFeatureのグローバルプロバイダを登録する(CONTRACT.mdセクション13)
//
// backend は元々 GORM 接続を持つため、bff(HTTP retrieverでbackendのexportエンドポイントをポーリングする)とは異なり、mysqlRetriever が直接クエリする
//
// PollingInterval ごとに再読込され、admin/go・admin/rails からの変更が backend の再起動無しに反映される
func NewMySQLEvaluator(ctx context.Context, featureFlagRepo *repository.FeatureFlag, pollingInterval time.Duration, logger *slog.Logger) (*Evaluator, error) {
	provider, err := gofeatureflaginprocess.NewProviderWithContext(ctx, gofeatureflaginprocess.ProviderOptions{
		GOFeatureFlagConfig: &ffclient.Config{
			PollingInterval: pollingInterval,
			Retriever:       &mysqlRetriever{repo: featureFlagRepo},
			FileFormat:      "json",
			Context:         ctx,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("MySQLベースのGO Feature Flag providerの初期化に失敗しました: %w", err)
	}

	if err := openfeature.SetProviderAndWait(provider); err != nil {
		return nil, fmt.Errorf("OpenFeature providerの登録に失敗しました: %w", err)
	}

	return &Evaluator{client: openfeature.NewClient("backend"), logger: logger}, nil
}
