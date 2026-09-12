// task_bob.go はCONTRACT.mdセクション24(backend内でのORM比較、GORM/bob)向けに追加した、
// 外部公開API(/external/v1/tasks)のTask一覧取得ロジックのbob(https://github.com/stephenafamo/bob)実装
//
// task.go の ListOffsetForExternalAPI・ListCursorForExternalAPI(GORM版)と
// 完全に同じ挙動(ソート順・絞り込み条件・total件数の扱い・Labelsの同時取得)になるよう
// 意図的に1行ずつ対応させながら実装している。以下、実装中に見つかったGORMとの違い
// (「ハマりどころ」)をこのファイルの先頭にまとめておく:
//
//  1. 【最大の違い】relation(Preload)の自動生成にはFK制約が必要
//     migrations/000004_create_task_labels.up.sql のコメントにある通り、task_labelsには
//     label_id/task_idへの外部キー制約が意図的に無い(GORMは構造体タグ(many2many:task_labels)
//     だけでPreload("Labels")を解決できるが、bobのコード生成はDBのFK制約からしか
//     リレーションを推測しない)。そのため bobgen-mysql は Task/Label の間に何の
//     リレーションヘルパー(Loader/Preload相当)も生成しなかった
//     (internal/repository/bobgen/models/tasks.bob.go に "Labels" に相当するフィールドが
//     無いことで確認できる。bob_loaders.bob.go・bob_joins.bob.go も中身は空の骨組みのみ)
//     → このファイルでは GORM の Preload が内部でやっていることと同じ「まとめてIN取得」を
//     attachLabelsBob で手動実装している(task_labelsをtask_id INで引き、続けて
//     labelsをlabel_id INで引く2クエリ。ページ内のtask件数によらずクエリ数は一定なので
//     N+1にはならない)
//
//  2. 型システムの違い: bobは *string の代わりに null.Val[string](aarondl/opt)、
//     BIGINT UNSIGNEDの代わりに types.Uint64(uint64の別名)を生成する
//     GORMのmodel.Task/model.Labelは素朴な *string/uint64 なので、
//     このファイルの中でだけ変換し、repository層より上(service/handler)からは
//     一切bob固有の型が見えないようにしている(戻り値は既存のmodel.Task)
//
//  3. クエリビルダの書き味: GORMは `.Order("created_at DESC, id DESC")` のような
//     生SQL文字列をそのまま渡せるが、bobは列ごとに型付きの sm.OrderBy(column).Desc() を
//     複数個積み重ねる設計になっている(列名のタイポがコンパイル時に検出できる代わりに、
//     複合ソートを1個の文字列で書けない)。WHERE句も同様に mysql.Arg(...) で明示的に
//     バインド変数化する必要があり、GORMの `Where("user_id = ?", v)` のようなプレースホルダ
//     文字列は使わない(その代わりSQLインジェクションの心配が構造的に無くなる)
//
//  4. コネクションプール: bobは database/sql の *sql.DB をそのままExecutorとして使う
//     (GORMのような独自ラッパーを挟まない)。新規にプールを作る必要は無く、
//     コンストラクタで受け取った既存の *gorm.DB が内部に保持している *sql.DB
//     (db.DB()で取得できる)をそのまま共有している
package repository

import (
	"context"
	"fmt"
	"sort"

	"github.com/stephenafamo/bob"
	"github.com/stephenafamo/bob/dialect/mysql"
	"github.com/stephenafamo/bob/dialect/mysql/dialect"
	"github.com/stephenafamo/bob/dialect/mysql/sm"

	"github.com/masahikokawai/training-go/bff-gin/backend/internal/model"
	bobmodels "github.com/masahikokawai/training-go/bff-gin/backend/internal/repository/bobgen/models"
)

// bobExecutor はbobのクエリ実行に使うExecutorを返す(上記コメント4参照)
//
// bob.NewDB(*sql.DB)は素の*sql.DBを bob.Executor 互換の薄いラッパー(bob.DB、
// フィールドとしてそのまま*sql.DBを埋め込むだけ)に変換するヘルパーで、新しい
// コネクションプールを作るわけではない(*sql.DBのQueryContextの戻り値の型が
// database/sql.Rows のままだとbobが期待するscan.Rowsインターフェースを満たさないための、
// 型を合わせるだけの薄いラップ)
func (r *Task) bobExecutor() (bob.Executor, error) {
	sqlDB, err := r.db.DB()
	if err != nil {
		return nil, fmt.Errorf("bob実行用のsql.DB取得に失敗: %w", err)
	}
	return bob.NewDB(sqlDB), nil
}

// ListOffsetForExternalAPIBob はListOffsetForExternalAPI(task.go)のbob版
// created_at DESC, id DESC で安定ソートする点、Limit/Offsetの計算方法、
// totalの数え方(Feature Flag/絞り込み条件と無関係にuser_idだけで絞った全件数)まで
// GORM版と完全に一致させている
func (r *Task) ListOffsetForExternalAPIBob(ctx context.Context, userID uint64, page, pageSize int) ([]model.Task, int64, error) {
	exec, err := r.bobExecutor()
	if err != nil {
		return nil, 0, err
	}

	total, err := bobmodels.Tasks.Query(
		sm.Where(bobmodels.Tasks.Columns.UserID.EQ(mysql.Arg(userID))),
	).Count(ctx, exec)
	if err != nil {
		return nil, 0, fmt.Errorf("タスク件数取得(外部API v1 bob): %w", err)
	}

	offset := (page - 1) * pageSize
	bobTasks, err := bobmodels.Tasks.Query(
		sm.Where(bobmodels.Tasks.Columns.UserID.EQ(mysql.Arg(userID))),
		sm.OrderBy(bobmodels.Tasks.Columns.CreatedAt).Desc(),
		sm.OrderBy(bobmodels.Tasks.Columns.ID).Desc(),
		sm.Limit(int64(pageSize)),
		sm.Offset(int64(offset)),
	).All(ctx, exec)
	if err != nil {
		return nil, 0, fmt.Errorf("タスク一覧取得(外部API v1 bob): %w", err)
	}

	tasks, err := r.attachLabelsBob(ctx, exec, bobTasks)
	if err != nil {
		return nil, 0, err
	}
	return tasks, total, nil
}

// ListCursorForExternalAPIBob はListCursorForExternalAPI(task.go)のbob版
// keysetの絞り込み条件 (created_at < ?) OR (created_at = ? AND id < ?) を、
// GORMでは生SQL文字列だったところをbobの型付きビルダ(mysql.Or/mysql.And + 列のLT/EQ)で
// 組み立てている点が最大の違い(コメント3参照)。生成されるSQL自体はGORM版と同一になる
func (r *Task) ListCursorForExternalAPIBob(ctx context.Context, userID uint64, cursorAfter *TaskCursor, limit int) ([]model.Task, error) {
	exec, err := r.bobExecutor()
	if err != nil {
		return nil, err
	}

	mods := []bob.Mod[*dialect.SelectQuery]{
		sm.Where(bobmodels.Tasks.Columns.UserID.EQ(mysql.Arg(userID))),
		sm.OrderBy(bobmodels.Tasks.Columns.CreatedAt).Desc(),
		sm.OrderBy(bobmodels.Tasks.Columns.ID).Desc(),
		sm.Limit(int64(limit)),
	}
	if cursorAfter != nil {
		mods = append(mods, sm.Where(
			mysql.Or(
				bobmodels.Tasks.Columns.CreatedAt.LT(mysql.Arg(cursorAfter.CreatedAt)),
				mysql.And(
					bobmodels.Tasks.Columns.CreatedAt.EQ(mysql.Arg(cursorAfter.CreatedAt)),
					bobmodels.Tasks.Columns.ID.LT(mysql.Arg(cursorAfter.ID)),
				),
			),
		))
	}

	bobTasks, err := bobmodels.Tasks.Query(mods...).All(ctx, exec)
	if err != nil {
		return nil, fmt.Errorf("タスク一覧取得(外部API v2 bob): %w", err)
	}

	return r.attachLabelsBob(ctx, exec, bobTasks)
}

// attachLabelsBob はGORMのPreload("Labels")相当を手動で再現する(コメント1参照)
//
// 1) 取得済みのtaskのIDでtask_labelsをIN検索してtask_id→[]label_idの対応表を作る
// 2) 出現したlabel_idをまとめてlabelsテーブルからIN検索する
// (どちらも「ページ内のtask件数」に対してクエリ回数が増えないため、N+1にはならない)
//
// 【順序についての注意】GORMのPreloadはtask_labelsの行が返る順(通常はauto_incrementの
// id昇順、つまり紐付けた順)でLabelsスライスを埋める。ここでも同じ挙動に合わせるため、
// label_idではなくtask_labels.id(付け替え順)でソートしてからLabelsを組み立てる
func (r *Task) attachLabelsBob(ctx context.Context, exec bob.Executor, bobTasks bobmodels.TaskSlice) ([]model.Task, error) {
	tasks := make([]model.Task, 0, len(bobTasks))
	if len(bobTasks) == 0 {
		return tasks, nil
	}

	taskIDArgs := make([]bob.Expression, 0, len(bobTasks))
	for _, t := range bobTasks {
		taskIDArgs = append(taskIDArgs, mysql.Arg(uint64(t.ID)))
	}

	taskLabels, err := bobmodels.TaskLabels.Query(
		sm.Where(bobmodels.TaskLabels.Columns.TaskID.In(taskIDArgs...)),
	).All(ctx, exec)
	if err != nil {
		return nil, fmt.Errorf("task_labels取得(外部API bob): %w", err)
	}
	// task_labels.id(付け替え順)で安定ソートしておく(上記コメント参照)
	sort.Slice(taskLabels, func(i, j int) bool { return taskLabels[i].ID < taskLabels[j].ID })

	labelIDsByTask := make(map[uint64][]uint64, len(bobTasks))
	labelIDSet := make(map[uint64]struct{})
	for _, tl := range taskLabels {
		taskID := uint64(tl.TaskID)
		labelID := uint64(tl.LabelID)
		labelIDsByTask[taskID] = append(labelIDsByTask[taskID], labelID)
		labelIDSet[labelID] = struct{}{}
	}

	labelsByID := make(map[uint64]model.Label, len(labelIDSet))
	if len(labelIDSet) > 0 {
		labelIDArgs := make([]bob.Expression, 0, len(labelIDSet))
		for id := range labelIDSet {
			labelIDArgs = append(labelIDArgs, mysql.Arg(id))
		}
		bobLabels, err := bobmodels.Labels.Query(
			sm.Where(bobmodels.Labels.Columns.ID.In(labelIDArgs...)),
		).All(ctx, exec)
		if err != nil {
			return nil, fmt.Errorf("labels取得(外部API bob): %w", err)
		}
		for _, l := range bobLabels {
			labelsByID[uint64(l.ID)] = model.Label{
				ID:        uint64(l.ID),
				Name:      l.Name,
				CreatedAt: l.CreatedAt,
				UpdatedAt: l.UpdatedAt,
			}
		}
	}

	for _, t := range bobTasks {
		taskID := uint64(t.ID)
		labelIDs := labelIDsByTask[taskID]
		labels := make([]model.Label, 0, len(labelIDs))
		for _, id := range labelIDs {
			labels = append(labels, labelsByID[id])
		}
		tasks = append(tasks, model.Task{
			ID:          taskID,
			Name:        t.Name,
			Description: t.Description.Ptr(),
			Status:      model.TaskStatus(t.Status),
			FinishedOn:  t.FinishedOn,
			UserID:      uint64(t.UserID),
			CreatedAt:   t.CreatedAt,
			UpdatedAt:   t.UpdatedAt,
			Labels:      labels,
		})
	}
	return tasks, nil
}
