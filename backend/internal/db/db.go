// Package db はGORMのコネクション初期化を担う
package db

import (
	"fmt"
	"log/slog"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"

	"github.com/masahikokawai/training-go/bff-gin/backend/internal/model"
)

// New はMySQLへのGORMコネクションを開く
//
// Rails対比: Rails.application.config.database.yml + ActiveRecord::Base.establish_connection に相当する
// GORM は接続プールを内部の *sql.DB として持つため、Close()も
// 呼び出し側で sql.DB 経由で行う(GORMのDBはただのラッパー)
//
// logger はGORMのSQLログをslogへ橋渡しする(newGormLogger, logger.go参照)
// LOG_LEVEL=debug で起動すると発行されたSQLがそのままログに出る
func New(dsn string, logger *slog.Logger) (*gorm.DB, error) {
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{
		Logger: newGormLogger(logger),
	})
	if err != nil {
		return nil, fmt.Errorf("DB接続に失敗: %w", err)
	}

	// 【実機検証で判明した不具合の修正】task_labelsはid/created_at/updated_atを
	// 持つテーブルだが、Task.Labels/Label.Tasksのmany2manyタグだけではGORMは
	// 「task_id/label_idの2列だけの単純な中間テーブル」だと仮定してしまい、
	// created_at/updated_at(NOT NULL、デフォルト値無し)を含めずにINSERTしようと
	// して "Field 'created_at' doesn't have a default value" で失敗していた
	// SetupJoinTableでmodel.TaskLabel(CreatedAt/UpdatedAtを持つ)を明示的に
	// 中間テーブルのモデルとして登録することで、GORMの自動タイムスタンプ機能が
	// task_labelsのINSERTにも適用されるようにする
	if err := db.SetupJoinTable(&model.Task{}, "Labels", &model.TaskLabel{}); err != nil {
		return nil, fmt.Errorf("task_labels join table設定に失敗: %w", err)
	}
	if err := db.SetupJoinTable(&model.Label{}, "Tasks", &model.TaskLabel{}); err != nil {
		return nil, fmt.Errorf("task_labels join table設定(Label側)に失敗: %w", err)
	}

	return db, nil
}
