package service

import (
	"encoding/base64"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
)

// cursorのencode/decodeはDB不要な純粋関数なので単体テストで確認する
// (offset/cursorページングのSQL発行自体はtest/integrationのMySQL結合テストで確認する)
func TestExternalCursorRoundTrip(t *testing.T) {
	createdAt := time.Date(2026, 9, 6, 12, 34, 56, 789000000, time.UTC)
	cursor := encodeExternalCursor(createdAt, 42)

	got, err := decodeExternalCursor(cursor)
	if err != nil {
		t.Fatalf("decodeExternalCursor() error = %v", err)
	}

	want := &struct {
		CreatedAt time.Time
		ID        uint64
	}{CreatedAt: createdAt, ID: 42}

	if diff := cmp.Diff(want.CreatedAt, got.CreatedAt); diff != "" {
		t.Errorf("CreatedAt mismatch (-want +got):\n%s", diff)
	}
	if got.ID != want.ID {
		t.Errorf("ID = %d, want %d", got.ID, want.ID)
	}
}

func TestDecodeExternalCursor_Invalid(t *testing.T) {
	// 【テスト監査で追記】decodeExternalCursorの内部には4つの異なる失敗分岐
	// (base64デコード失敗/区切り"|"が無い/created_atの形式不正/idの形式不正)があるが、元々のテストは前者2つしかカバーしていなかった
	//
	// 区切りは正しいが中身が壊れている
	// パターン(実際に外部クライアントが不正なcursorを手で組み立てて送ってくるようなケースを想定)を追加する
	validSep := func(before, after string) string {
		return base64.URLEncoding.EncodeToString([]byte(before + "|" + after))
	}
	cases := map[string]string{
		"空文字":                         "",
		"base64として不正":                 "not-base64!!!",
		"区切り「|」が無い":                   "aW52YWxpZA==", // "invalid" をbase64化しただけ
		"created_atがRFC3339Nano形式でない": validSep("not-a-timestamp", "42"),
		"idが数値でない":                    validSep(time.Now().Format(time.RFC3339Nano), "not-a-number"),
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := decodeExternalCursor(c); err == nil {
				t.Errorf("decodeExternalCursor(%q) expected error, got nil", c)
			}
		})
	}
}
