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
// BooleanEvaluationの戻り値を固定できるため、実際のGO Feature Flag(HTTP retriever等)を
// 経由せずにEvaluator.BoolValueのロジック(ログ出力・defaultValueへのフォールバック)だけを
// 単体テストできる
type fakeProvider struct {
	value bool
	// strValue はStringEvaluationの戻り値を固定するためのフィールド
	// (CONTRACT.mdセクション19: Evaluator.StringValueのテスト用に追加)
	strValue string
	err      error
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

func (p fakeProvider) StringEvaluation(_ context.Context, _ string, defaultValue string, _ openfeature.FlattenedContext) openfeature.StringResolutionDetail {
	if p.err != nil {
		return openfeature.StringResolutionDetail{
			Value: defaultValue,
			ProviderResolutionDetail: openfeature.ProviderResolutionDetail{
				ResolutionError: openfeature.NewGeneralResolutionError(p.err.Error()),
			},
		}
	}
	if p.strValue != "" {
		return openfeature.StringResolutionDetail{Value: p.strValue}
	}
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

// newTestEvaluator はfakeProviderをOpenFeatureのグローバルprovider(クライアント名固定)に
// 差し替えたEvaluatorを組み立てる。テストごとにクライアント名を変え、他テストの
// provider登録と干渉しないようにする
func newTestEvaluator(t *testing.T, clientName string, value bool, err error) (*Evaluator, *bytes.Buffer) {
	t.Helper()
	if setErr := openfeature.SetNamedProviderAndWait(clientName, fakeProvider{value: value, err: err}); setErr != nil {
		t.Fatalf("provider登録に失敗: %v", setErr)
	}
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	return &Evaluator{client: openfeature.NewClient(clientName), logger: logger}, &buf
}

// newTestEvaluatorString はStringValue用。newTestEvaluatorと役割は同じだが、
// fakeProviderにstrValue/errを渡す必要があるため別関数にしている
func newTestEvaluatorString(t *testing.T, clientName string, strValue string, err error) (*Evaluator, *bytes.Buffer) {
	t.Helper()
	if setErr := openfeature.SetNamedProviderAndWait(clientName, fakeProvider{strValue: strValue, err: err}); setErr != nil {
		t.Fatalf("provider登録に失敗: %v", setErr)
	}
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	return &Evaluator{client: openfeature.NewClient(clientName), logger: logger}, &buf
}

// TestEvaluator_StringValue はCONTRACT.mdセクション19: frontend.task-create-uxのような
// 多値(multivariate)フラグの評価(BoolValueと対称的なロジック、ログ出力含む)を検証する
func TestEvaluator_StringValue_ログに評価結果を残す(t *testing.T) {
	tests := []struct {
		name         string
		flagValue    string
		defaultValue string
	}{
		{name: "評価結果がmodal", flagValue: "modal", defaultValue: "inline"},
		{name: "評価結果がpage", flagValue: "page", defaultValue: "inline"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e, buf := newTestEvaluatorString(t, "test-string-"+tt.name, tt.flagValue, nil)

			got := e.StringValue(context.Background(), "frontend.task-create-ux", tt.defaultValue, "user-123")
			if got != tt.flagValue {
				t.Fatalf("StringValue() = %v, want %v", got, tt.flagValue)
			}

			log := buf.String()
			for _, want := range []string{
				`flag=frontend.task-create-ux`,
				`user_id=user-123`,
				`value=` + tt.flagValue,
			} {
				if !strings.Contains(log, want) {
					t.Errorf("ログに %q が含まれていない。log=%s", want, log)
				}
			}
		})
	}
}

func TestEvaluator_StringValue_評価エラー時はdefaultValueを返しWARNログを残す(t *testing.T) {
	e, buf := newTestEvaluatorString(t, "test-string-error", "", errFake)

	got := e.StringValue(context.Background(), "frontend.task-create-ux", "inline", "user-9")
	if got != "inline" {
		t.Fatalf("StringValue() = %v, want defaultValue(inline)", got)
	}

	log := buf.String()
	if !strings.Contains(log, "level=WARN") {
		t.Errorf("エラー時はWARNレベルでログを残すべき。log=%s", log)
	}
	if !strings.Contains(log, `flag=frontend.task-create-ux`) || !strings.Contains(log, `user_id=user-9`) {
		t.Errorf("ログにflag/user_idが含まれていない。log=%s", log)
	}
}

// Loggerを渡さない(nil)構築でもpanicしないことを確認する回帰テスト(BoolValue版と対称)
func TestEvaluator_StringValue_Loggerがnilでもpanicしない(t *testing.T) {
	if setErr := openfeature.SetNamedProviderAndWait("test-string-nil-logger", fakeProvider{strValue: "modal"}); setErr != nil {
		t.Fatalf("provider登録に失敗: %v", setErr)
	}
	e := &Evaluator{client: openfeature.NewClient("test-string-nil-logger")}

	got := e.StringValue(context.Background(), "any.flag", "inline", "user-1")
	if got != "modal" {
		t.Fatalf("StringValue() = %v, want modal", got)
	}
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

			got := e.BoolValue(context.Background(), "frontend.tasks-ts-rewrite", tt.defaultValue, "user-123")
			if got != tt.flagValue {
				t.Fatalf("BoolValue() = %v, want %v", got, tt.flagValue)
			}

			log := buf.String()
			for _, want := range []string{
				`flag=frontend.tasks-ts-rewrite`,
				`user_id=user-123`,
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

	got := e.BoolValue(context.Background(), "bff.tasks-backend-v2", true, "user-9")
	if got != true {
		t.Fatalf("BoolValue() = %v, want defaultValue(true)", got)
	}

	log := buf.String()
	if !strings.Contains(log, "level=WARN") {
		t.Errorf("エラー時はWARNレベルでログを残すべき。log=%s", log)
	}
	if !strings.Contains(log, `flag=bff.tasks-backend-v2`) || !strings.Contains(log, `user_id=user-9`) {
		t.Errorf("ログにflag/user_idが含まれていない。log=%s", log)
	}
}

// Loggerを渡さない(nil)構築でもpanicしないことを確認する回帰テスト
// bffのproxy.TaskRoutes.Loggerと同様、既存コード・既存テストがlogger省略で
// 構築しているケースへの後方互換を壊さないため
func TestEvaluator_BoolValue_Loggerがnilでもpanicしない(t *testing.T) {
	if setErr := openfeature.SetNamedProviderAndWait("test-bool-nil-logger", fakeProvider{value: true}); setErr != nil {
		t.Fatalf("provider登録に失敗: %v", setErr)
	}
	e := &Evaluator{client: openfeature.NewClient("test-bool-nil-logger")}

	got := e.BoolValue(context.Background(), "any.flag", false, "user-1")
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
