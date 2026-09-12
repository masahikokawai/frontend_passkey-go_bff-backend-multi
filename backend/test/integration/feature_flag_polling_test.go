//go:build integration

// admin/go・admin/rails経由でのfeature_flagsテーブル更新が、backend自身の
// MySQLベースのEvaluator(NewMySQLEvaluator、内部でmysqlRetrieverがPollingInterval
// ごとにDBを再読込する)に実際に反映されるまでの経路を検証する
// 「人間が実際にadmin画面でフラグを切り替えたときに反映されるか」という、
// これまでの単体テスト(BuildFlagConfigJSONの純粋なJSON変換ロジックのみ)では
// カバーできていなかった、DBの実データ変更→再ポーリング→評価結果反映、という
// 一連の流れを実MySQLで確認する
// 実行前提: 環境変数 TEST_DB_DSN(README参照)
package integration

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/masahikokawai/training-go/bff-gin/backend/internal/db"
	"github.com/masahikokawai/training-go/bff-gin/backend/internal/featureflag"
	"github.com/masahikokawai/training-go/bff-gin/backend/internal/repository"
)

func TestFeatureFlagEvaluator_ReflectsDBChangeAfterPolling(t *testing.T) {
	dsn := os.Getenv("TEST_DB_DSN")
	if dsn == "" {
		t.Skip("TEST_DB_DSN未設定のためスキップ(README参照)")
	}

	gormDB, err := db.New(dsn, slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	if err != nil {
		t.Fatalf("DB接続失敗: %v", err)
	}
	// 【テスト監査で発見・修正】このDB接続をCloseせずにテストが終わっていたため、
	// 実行のたびに接続がリークしていた(internal/db/db.goのコメントで呼び出し側の責務と明記)
	if sqlDB, sqlErr := gormDB.DB(); sqlErr == nil {
		t.Cleanup(func() { _ = sqlDB.Close() })
	}

	// migrations/000007_seed_feature_flags.up.sqlでseedされる既存フラグを使う
	// (admin/go・admin/railsが実際に操作するのと同じ行)
	const flagKey = "backend.external-tasks-pagination-v2"

	type row struct {
		Enabled          bool
		DefaultVariation string
	}
	readRow := func() row {
		var r row
		if err := gormDB.Table("feature_flags").
			Select("enabled", "default_variation").
			Where("flag_key = ?", flagKey).
			Scan(&r).Error; err != nil {
			t.Fatalf("feature_flags読み取り失敗: %v", err)
		}
		return r
	}
	writeRow := func(r row) {
		if err := gormDB.Table("feature_flags").
			Where("flag_key = ?", flagKey).
			Updates(map[string]any{"enabled": r.Enabled, "default_variation": r.DefaultVariation}).Error; err != nil {
			t.Fatalf("feature_flags更新失敗: %v", err)
		}
	}

	// admin/go・admin/railsも同じseed行を直接更新するため、他のテスト・手動確認との
	// 干渉を避けるべく必ず元の値へ戻す(このテストの後にユーザーが手動で環境構築・
	// 動作確認する予定があるため、フラグの既定状態を壊したままにしないこと)
	original := readRow()
	defer writeRow(original)

	// 【GO Feature Flagの挙動として判明】Disable(=!Enabled)がtrueのフラグは
	// 「defaultRule.variation を評価せず、常に BoolValue 呼び出し側が渡した defaultValue をそのまま返す」(SdkDefault)
	// そのためこのテストでは enabled=true に固定し、default_variation の値(admin画面での典型的な切り替え操作)だけを変えることで、
	// 「defaultValueへのフォールバックではなく、実際にDBの値が評価されている」ことを曖昧さ無く確認する
	writeRow(row{Enabled: true, DefaultVariation: "off"})

	featureFlagRepo := repository.NewFeatureFlag(gormDB)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	const pollInterval = 200 * time.Millisecond
	evaluator, err := featureflag.NewMySQLEvaluator(ctx, featureFlagRepo, pollInterval, nil)
	if err != nil {
		t.Fatalf("NewMySQLEvaluator失敗: %v", err)
	}

	// defaultValueをtrueにしても、DBの実際の評価結果(off=false)が返ることを確認する
	// (defaultValueへのフォールバックと区別するため、あえて食い違う値を渡す)
	if got := evaluator.BoolValue(ctx, flagKey, true, "test-client"); got != false {
		t.Fatalf("初回評価 = %v, want false(DBはdefault_variation=off)", got)
	}

	// admin/go・admin/rails が行うのと同じ「DB直接更新」を模して、
	// default_variation=on に切り替える
	writeRow(row{Enabled: true, DefaultVariation: "on"})

	// PollingIntervalの2周期分待ってから再評価する(ポーリングタイミングの
	// ジッタを吸収するための余裕)
	deadline := time.Now().Add(pollInterval * 10)
	for {
		got := evaluator.BoolValue(ctx, flagKey, false, "test-client")
		if got {
			break // 反映された
		}
		if time.Now().After(deadline) {
			t.Fatalf("DB更新(enabled=true)後、%v待ってもEvaluatorの評価結果に反映されなかった", pollInterval*10)
		}
		time.Sleep(pollInterval / 2)
	}
}
