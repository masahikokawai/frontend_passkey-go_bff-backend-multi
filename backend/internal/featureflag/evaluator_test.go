package featureflag

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/open-feature/go-sdk/openfeature"
)

// fakeProvider はopenfeature.FeatureProviderの最小フェイク実装
// BooleanEvaluationの戻り値を固定できるため、実際のMySQL retriever等を経由せずに
// Evaluator.BoolValueのロジック(ログ出力・defaultValueへのフォールバック)だけを
// 単体テストできる
type fakeProvider struct {
	value bool
	err   error
}

func (fakeProvider) Metadata() openfeature.Metadata { return openfeature.Metadata{Name: "fake"} }

func (p fakeProvider) BooleanEvaluation(_ context.Context, _ string, defaultValue bool, _ openfeature.FlattenedContext) openfeature.BoolResolutionDetail {
	if p.err != nil {
		return openfeature.BoolResolutionDetail{
			Value: defaultValue,
			ProviderResolutionDetail: openfeature.ProviderResolutionDetail{
				ResolutionError: openfeature.NewGeneralResolutionError(p.err.Error()),
			},
		}
	}
	return openfeature.BoolResolutionDetail{Value: p.value}
}

func (fakeProvider) StringEvaluation(_ context.Context, _ string, defaultValue string, _ openfeature.FlattenedContext) openfeature.StringResolutionDetail {
	return openfeature.StringResolutionDetail{Value: defaultValue}
}

func (fakeProvider) FloatEvaluation(_ context.Context, _ string, defaultValue float64, _ openfeature.FlattenedContext) openfeature.FloatResolutionDetail {
	return openfeature.FloatResolutionDetail{Value: defaultValue}
}

func (fakeProvider) IntEvaluation(_ context.Context, _ string, defaultValue int64, _ openfeature.FlattenedContext) openfeature.IntResolutionDetail {
	return openfeature.IntResolutionDetail{Value: defaultValue}
}

func (fakeProvider) ObjectEvaluation(_ context.Context, _ string, defaultValue any, _ openfeature.FlattenedContext) openfeature.InterfaceResolutionDetail {
	return openfeature.InterfaceResolutionDetail{Value: defaultValue}
}

func (fakeProvider) Hooks() []openfeature.Hook { return nil }

func newTestEvaluator(t *testing.T, clientName string, value bool, err error) (*Evaluator, *bytes.Buffer) {
	t.Helper()
	if setErr := openfeature.SetNamedProviderAndWait(clientName, fakeProvider{value: value, err: err}); setErr != nil {
		t.Fatalf("provider登録に失敗: %v", setErr)
	}
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	return &Evaluator{client: openfeature.NewClient(clientName), logger: logger}, &buf
}

func TestEvaluator_BoolValue_ログに評価結果を残す(t *testing.T) {
	tests := []struct {
		name         string
		flagValue    bool
		defaultValue bool
	}{
		{name: "評価結果がtrue", flagValue: true, defaultValue: false},
		{name: "評価結果がfalse", flagValue: false, defaultValue: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e, buf := newTestEvaluator(t, "test-bool-"+tt.name, tt.flagValue, nil)

			got := e.BoolValue(context.Background(), "backend.external-tasks-pagination-v2", tt.defaultValue, "external-api-client")
			if got != tt.flagValue {
				t.Fatalf("BoolValue() = %v, want %v", got, tt.flagValue)
			}

			log := buf.String()
			for _, want := range []string{
				`flag=backend.external-tasks-pagination-v2`,
				`targeting_key=external-api-client`,
			} {
				if !strings.Contains(log, want) {
					t.Errorf("ログに %q が含まれていない。log=%s", want, log)
				}
			}
			if !strings.Contains(log, "value="+boolString(tt.flagValue)) {
				t.Errorf("ログに評価結果(value=%v)が含まれていない。log=%s", tt.flagValue, log)
			}
		})
	}
}

func TestEvaluator_BoolValue_評価エラー時はdefaultValueを返しWARNログを残す(t *testing.T) {
	e, buf := newTestEvaluator(t, "test-bool-error", false, errFake)

	got := e.BoolValue(context.Background(), "backend.external-tasks-pagination-v2", true, "client-x")
	if got != true {
		t.Fatalf("BoolValue() = %v, want defaultValue(true)", got)
	}

	log := buf.String()
	if !strings.Contains(log, "level=WARN") {
		t.Errorf("エラー時はWARNレベルでログを残すべき。log=%s", log)
	}
	if !strings.Contains(log, `flag=backend.external-tasks-pagination-v2`) || !strings.Contains(log, `targeting_key=client-x`) {
		t.Errorf("ログにflag/targeting_keyが含まれていない。log=%s", log)
	}
}

// Loggerを渡さない(nil)構築でもpanicしないことを確認する回帰テスト
func TestEvaluator_BoolValue_Loggerがnilでもpanicしない(t *testing.T) {
	if setErr := openfeature.SetNamedProviderAndWait("test-bool-nil-logger", fakeProvider{value: true}); setErr != nil {
		t.Fatalf("provider登録に失敗: %v", setErr)
	}
	e := &Evaluator{client: openfeature.NewClient("test-bool-nil-logger")}

	got := e.BoolValue(context.Background(), "any.flag", false, "client-1")
	if got != true {
		t.Fatalf("BoolValue() = %v, want true", got)
	}
}

var errFake = fakeErr("fake resolution error")

type fakeErr string

func (e fakeErr) Error() string { return string(e) }

func boolString(b bool) string {
	if b {
		return "true"
	}
	return "false"
}
