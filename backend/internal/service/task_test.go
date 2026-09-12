package service

import (
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/masahikokawai/training-go/bff-gin/backend/internal/model"
)

// テーブル駆動テスト
// testify(assert/require)は使わずgoogle/go-cmpで比較する
// (jinjer-ats方針踏襲、CONTRACT.md/training-go/gin/internal/service/task_test.goと同方針)
// DBを使わない純粋な関数(validateTaskInput)のテストなので、Goツールチェーンさえあれば
// このファイル単体で `go test ./internal/service/...` が通る
func TestValidateTaskInput(t *testing.T) {
	tomorrow := today().AddDate(0, 0, 1)
	yesterday := today().AddDate(0, 0, -1)

	tests := []struct {
		name    string
		input   TaskInput
		wantErr bool
	}{
		{
			name:  "正常系",
			input: TaskInput{Name: "買い物", Status: "waiting", FinishedOn: tomorrow},
		},
		{
			name:    "name未入力はエラー",
			input:   TaskInput{Name: "", Status: "waiting", FinishedOn: tomorrow},
			wantErr: true,
		},
		{
			name:    "nameが20文字を超えるとエラー",
			input:   TaskInput{Name: "123456789012345678901", Status: "waiting", FinishedOn: tomorrow},
			wantErr: true,
		},
		{
			name:  "nameがちょうど20文字は許容される",
			input: TaskInput{Name: "12345678901234567890", Status: "waiting", FinishedOn: tomorrow},
		},
		{
			// ASCII文字だけの境界値テスト(上記2件)ではバイト数カウントでもルーン数カウントでも
			// 結果が同じになってしまい、len([]rune(...))を使っていることの検証にならない
			// マルチバイト文字(1文字3バイト)でちょうど20文字・21文字を試すことで、
			// 実装がバイト数ではなくルーン数で数えていることを実際に確認する
			name:  "マルチバイト文字でちょうど20文字は許容される(バイト数ではなくルーン数で判定)",
			input: TaskInput{Name: "あいうえおかきくけこさしすせそたちつてと", Status: "waiting", FinishedOn: tomorrow},
		},
		{
			name:    "マルチバイト文字で21文字はエラー(バイト数ではなくルーン数で判定)",
			input:   TaskInput{Name: "あいうえおかきくけこさしすせそたちつてとな", Status: "waiting", FinishedOn: tomorrow},
			wantErr: true,
		},
		{
			name:    "finished_onが過去日はエラー",
			input:   TaskInput{Name: "タスク", Status: "waiting", FinishedOn: yesterday},
			wantErr: true,
		},
		{
			name:  "finished_onが今日は許容される",
			input: TaskInput{Name: "タスク", Status: "waiting", FinishedOn: today()},
		},
		{
			name:    "不明なstatusはエラー",
			input:   TaskInput{Name: "タスク", Status: "unknown", FinishedOn: tomorrow},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateTaskInput(tt.input)
			gotErr := err != nil
			if diff := cmp.Diff(tt.wantErr, gotErr); diff != "" {
				t.Errorf("validateTaskInput() エラー有無の差分 (-want +got):\n%s\nerr=%v", diff, err)
			}
		})
	}
}

// 【テスト監査で発見・修正した実バグの回帰テスト】
// today()はUTC基準でなければならない(finished_on自体がハンドラでtime.Parseにより
// UTC基準でパースされているため、比較対象を合わせる必要がある。Rust/Railsも明示的に
// UTC基準で「今日」を計算しており、Go側だけサーバーのローカルタイムゾーン
// (time.Local、実行環境のTZ次第で不定)を基準にしていると、
// 同一リクエスト・同一時刻でも他言語実装と受理/拒否の判定が割れる契約違反になる
// (例: サーバーがJST(UTC+9)の場合、UTC 15:00〜23:59の間はGoだけ「今日」の判定が
// 他言語より1日進んでしまい、本来受理されるべきfinished_onを過去日として誤って拒否する)
func TestToday_UTC基準である(t *testing.T) {
	got := today()
	if got.Location() != time.UTC {
		t.Errorf("today().Location() = %v, want time.UTC(サーバーのタイムゾーンに依存しない基準であること)", got.Location())
	}
	if got.Hour() != 0 || got.Minute() != 0 || got.Second() != 0 || got.Nanosecond() != 0 {
		t.Errorf("today() = %v, 時刻部分は0であるべき(日付のみの比較基準のため)", got)
	}
}

func TestToTaskDTO_LabelsMapping(t *testing.T) {
	// toTaskDTOはmodel.Task→TaskDTOの変換のみを行う純粋関数なのでDB無しでテストできる
	// N+1/Preloadの挙動差そのものはtest/integrationの責務(要MySQL)
	task := model.Task{
		ID:   1,
		Name: "買い物",
		Labels: []model.Label{
			{ID: 1, Name: "急ぎ"},
			{ID: 2, Name: "家事"},
		},
	}
	got := toTaskDTO(task)
	want := []LabelDTO{{ID: 1, Name: "急ぎ"}, {ID: 2, Name: "家事"}}
	if diff := cmp.Diff(want, got.Labels); diff != "" {
		t.Errorf("toTaskDTO().Labels 差分 (-want +got):\n%s", diff)
	}
}
