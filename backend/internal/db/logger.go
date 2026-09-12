package db

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	mysqldriver "github.com/go-sql-driver/mysql"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// gormLogger はGORMのログをlog/slogへ流す
// training-go/gin(参照元)の internal/db/logger.go と同じ考え方
// (GORMのlogger.Interfaceを実装してslogへブリッジする)
//
// 学習用のためqueryTimeoutからの動的なslowThreshold算出は行わず、固定のslowThresholdにして構成を単純化している
//
// SQL本文は値がインライン展開された状態で渡ってくるため、個人情報を含みうる
// そのためSQL本文はdebugレベルでのみ出力し、info以上では出力しない
// ローカル開発で発行クエリを確認したい場合は LOG_LEVEL=debug で起動する
// (cmd/server/main.go参照)
type gormLogger struct {
	logger *slog.Logger
	level  gormlogger.LogLevel

	slowThreshold time.Duration
}

func newGormLogger(logger *slog.Logger) gormlogger.Interface {
	return &gormLogger{
		logger:        logger,
		level:         gormlogger.Warn,
		slowThreshold: 500 * time.Millisecond,
	}
}

func (l *gormLogger) LogMode(level gormlogger.LogLevel) gormlogger.Interface {
	clone := *l
	clone.level = level
	return &clone
}

const gormMessage = "GORMからの通知"

func (l *gormLogger) Info(ctx context.Context, msg string, data ...any) {
	if l.level < gormlogger.Info {
		return
	}
	l.logger.InfoContext(ctx, gormMessage, slog.String("detail", fmt.Sprintf(msg, data...)))
}

func (l *gormLogger) Warn(ctx context.Context, msg string, data ...any) {
	if l.level < gormlogger.Warn {
		return
	}
	l.logger.WarnContext(ctx, gormMessage, slog.String("detail", fmt.Sprintf(msg, data...)))
}

func (l *gormLogger) Error(ctx context.Context, msg string, data ...any) {
	if l.level < gormlogger.Error {
		return
	}
	l.logger.ErrorContext(ctx, gormMessage, slog.String("detail", fmt.Sprintf(msg, data...)))
}

// Trace は1クエリの実行結果を記録する
//
// GORM は全てのクエリの後にこれを呼ぶため、Railsで言えば `ActiveSupport::Notifications.subscribe("sql.active_record")`でSQLログを横取りしているのに近い
func (l *gormLogger) Trace(ctx context.Context, begin time.Time, fc func() (string, int64), err error) {
	if l.level <= gormlogger.Silent {
		return
	}

	elapsed := time.Since(begin)
	attrs := []any{slog.String("duration", elapsed.String())}

	// SQL本文には値がインライン展開されており個人情報を含みうるため、
	// debugが有効なときだけ付与する
	if l.logger.Enabled(ctx, slog.LevelDebug) {
		sql, rows := fc()
		attrs = append(attrs, slog.String("sql", sql), slog.Int64("rows", rows))
	}

	switch {
	case err != nil && !errors.Is(err, gorm.ErrRecordNotFound):
		attrs = append(attrs, slog.String("error", safeDBError(ctx, l.logger, err)))
		l.logger.ErrorContext(ctx, "クエリが失敗した", attrs...)
	case elapsed > l.slowThreshold:
		attrs = append(attrs, slog.String("slow_threshold", l.slowThreshold.String()))
		l.logger.WarnContext(ctx, "スロークエリ", attrs...)
	default:
		l.logger.DebugContext(ctx, "クエリを実行した", attrs...)
	}
}

// safeDBError はinfo以上のレベルで出して良いエラー表現を返す
// MySQLのサーバエラーはメッセージ本文に違反した値やSQL断片を含みうるため、
// debugが有効なときだけ全文を出し、それ以外はエラー番号とSQLSTATEに丸める
func safeDBError(ctx context.Context, logger *slog.Logger, err error) string {
	if logger.Enabled(ctx, slog.LevelDebug) {
		return err.Error()
	}
	var mysqlErr *mysqldriver.MySQLError
	if errors.As(err, &mysqlErr) {
		return fmt.Sprintf("MySQL error %d (%s)", mysqlErr.Number, mysqlErr.SQLState)
	}
	return err.Error()
}
