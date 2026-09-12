// Package db はGORMのコネクション初期化を担う
// backend/internal/db/db.goと同じ考え方だが、
// このアプリはFeature Flagの読み書きしか行わずSQLログの細かい制御は不要なため、
// GORMのデフォルトロガーをそのまま使う(backendほど厳密なslogブリッジは不要と判断)
package db

import (
	"fmt"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

// New はMySQLへのGORMコネクションを開く
func New(dsn string) (*gorm.DB, error) {
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{})
	if err != nil {
		return nil, fmt.Errorf("DB接続に失敗: %w", err)
	}
	return db, nil
}
