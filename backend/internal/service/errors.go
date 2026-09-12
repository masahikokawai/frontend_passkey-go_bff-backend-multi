package service

import "errors"

// Sentinel Error方式
//
// Rails対比: Rails/ActiveRecordは `raise ActiveRecord::RecordInvalid` のように
// 例外階層で表現するが、Goは例外機構を持たないため「呼び出し元がerrors.Isで判定できる
// パッケージレベルの変数」として定義するのが定石(training-go/gin/internal/service/errors.goと同方針)
var (
	ErrNotFound   = errors.New("対象が見つかりません")
	ErrValidation = errors.New("入力値が不正です")
	ErrForbidden  = errors.New("この操作を行う権限がありません")
	// ErrLastManagerUser はUpdateRole(management→general降格)・Delete(削除)いずれでも、
	// それが最後のmanagementユーザーだった場合に共通で返す(CONTRACT.mdセクション17.3:
	// Deleteのガードロジックを重複実装せずUpdateRoleと共有するため、メッセージも
	// 操作を限定しない汎用的な文言にしてある)
	ErrLastManagerUser = errors.New("最後の管理者を変更・削除することはできません")
	ErrEmailTaken      = errors.New("このメールアドレスは既に使われています")
)
