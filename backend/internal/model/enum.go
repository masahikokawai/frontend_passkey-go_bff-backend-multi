package model

import (
	"database/sql/driver"
	"fmt"
	"strconv"
)

// scanUint8 は training-go/gin/internal/model/enum.go と同じ考え方
// Rails/ActiveRecordのenumはDB上の整数とアプリ上のシンボルを自動変換するが、
// Goには相当の仕組みが無いため、sql.Scanner / driver.Valuer を自分で実装する
func scanUint8(value any) (uint8, error) {
	switch v := value.(type) {
	case nil:
		return 0, nil
	case int64:
		if v < 0 || v > 255 {
			return 0, fmt.Errorf("値 %d は uint8 の範囲外", v)
		}
		return uint8(v), nil
	case uint64:
		if v > 255 {
			return 0, fmt.Errorf("値 %d は uint8 の範囲外", v)
		}
		return uint8(v), nil
	case []byte:
		n, err := strconv.ParseUint(string(v), 10, 8)
		if err != nil {
			return 0, fmt.Errorf("バイト列 %q を数値に変換できない: %w", v, err)
		}
		return uint8(n), nil
	case string:
		n, err := strconv.ParseUint(v, 10, 8)
		if err != nil {
			return 0, fmt.Errorf("文字列 %q を数値に変換できない: %w", v, err)
		}
		return uint8(n), nil
	default:
		return 0, fmt.Errorf("サポートしていない型 %T の値 %v", value, value)
	}
}

// Role は Rails の `enum role: { general: 1, management: 2 }, _prefix: true` の再現
// CONTRACT.md セクション10の通り、role自体はアプリ側で保持し続ける
// (Keycloakのrealm roleとの同期はJITプロビジョニング時の初期反映のみ)
type Role uint8

const (
	RoleGeneral    Role = 1
	RoleManagement Role = 2
)

func (r Role) String() string {
	switch r {
	case RoleGeneral:
		return "general"
	case RoleManagement:
		return "management"
	default:
		return fmt.Sprintf("unknown(%d)", uint8(r))
	}
}

func (r Role) Valid() bool {
	return r == RoleGeneral || r == RoleManagement
}

func (r *Role) Scan(value any) error {
	v, err := scanUint8(value)
	if err != nil {
		return fmt.Errorf("Role.Scan: %w", err)
	}
	*r = Role(v)
	return nil
}

func (r Role) Value() (driver.Value, error) {
	return int64(r), nil
}

// RoleFromString はKeycloakのrealm role名(例: "management")からRoleへ変換する
func RoleFromString(s string) (Role, error) {
	switch s {
	case "general":
		return RoleGeneral, nil
	case "management":
		return RoleManagement, nil
	default:
		return 0, fmt.Errorf("不明なrole: %q", s)
	}
}

// TaskStatus は Rails の `enum status: { waiting: 1, work_in_progress: 2, completed: 3 }` の再現
type TaskStatus uint8

const (
	TaskStatusWaiting        TaskStatus = 1
	TaskStatusWorkInProgress TaskStatus = 2
	TaskStatusCompleted      TaskStatus = 3
)

func (s TaskStatus) String() string {
	switch s {
	case TaskStatusWaiting:
		return "waiting"
	case TaskStatusWorkInProgress:
		return "work_in_progress"
	case TaskStatusCompleted:
		return "completed"
	default:
		return fmt.Sprintf("unknown(%d)", uint8(s))
	}
}

func (s TaskStatus) Valid() bool {
	switch s {
	case TaskStatusWaiting, TaskStatusWorkInProgress, TaskStatusCompleted:
		return true
	default:
		return false
	}
}

func (s *TaskStatus) Scan(value any) error {
	v, err := scanUint8(value)
	if err != nil {
		return fmt.Errorf("TaskStatus.Scan: %w", err)
	}
	*s = TaskStatus(v)
	return nil
}

func (s TaskStatus) Value() (driver.Value, error) {
	return int64(s), nil
}

// TaskStatusFromString はAPI入力(JSONのstatus文字列)からTaskStatusへ変換する
func TaskStatusFromString(s string) (TaskStatus, error) {
	switch s {
	case "waiting":
		return TaskStatusWaiting, nil
	case "work_in_progress":
		return TaskStatusWorkInProgress, nil
	case "completed":
		return TaskStatusCompleted, nil
	default:
		return 0, fmt.Errorf("不明なstatus: %q", s)
	}
}
