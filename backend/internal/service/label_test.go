package service

import "testing"

// テーブル駆動テスト
// DBを使わない純粋な関数(validateLabelName)のテストなので、
// task_test.go(validateTaskInput)と同じ方針で、Goツールチェーンさえあれば
// このファイル単体で `go test ./internal/service/...` が通る
//
// これまでLabelService(name必須・10文字以内)のバリデーションは一切テストが
// 無かった(repository層はDB接続が必要なため素朴にはテストしづらく、見落とされていた)
func TestValidateLabelName(t *testing.T) {
	tests := []struct {
		name      string
		labelName string
		wantErr   bool
	}{
		{name: "正常系", labelName: "重要"},
		{name: "name未入力はエラー", labelName: "", wantErr: true},
		{name: "nameが10文字を超えるとエラー", labelName: "12345678901", wantErr: true},
		{name: "nameがちょうど10文字は許容される", labelName: "1234567890"},
		{
			// ASCII文字だけの境界値テスト(上記2件)ではバイト数カウントでもルーン数カウントでも
			// 結果が同じになってしまい、len([]rune(...))を使っていることの検証にならない
			// マルチバイト文字(1文字3バイト)でちょうど10文字・11文字を試すことで、
			// 実装がバイト数ではなくルーン数で数えていることを実際に確認する
			name:      "マルチバイト文字でちょうど10文字は許容される(バイト数ではなくルーン数で判定)",
			labelName: "あいうえおかきくけこ",
		},
		{
			name:      "マルチバイト文字で11文字はエラー(バイト数ではなくルーン数で判定)",
			labelName: "あいうえおかきくけこさ",
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateLabelName(tt.labelName)
			if tt.wantErr && err == nil {
				t.Fatal("エラーを期待したがnilだった")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("エラーを期待していないが: %v", err)
			}
		})
	}
}
