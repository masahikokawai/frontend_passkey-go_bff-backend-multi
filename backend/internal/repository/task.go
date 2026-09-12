// Package repository はDBアクセスを担う層
// http→handler→service→repository→db というこのプロジェクト全体の層構造のうち最下層(dbの直前)にあたる
package repository

import (
	"context"
	"fmt"
	"time"

	"gorm.io/gorm"

	"github.com/masahikokawai/training-go/bff-gin/backend/internal/model"
)

// Task はtasksテーブルへのアクセスを担当する
//
// CONTRACT.md セクション5の通り、Preloadを使うか使わないか(N+1になるか
// ならないか)は「repositoryではなくservice層で制御する」方針のため、
// repositoryはPreloadあり/なしの両方のプリミティブを提供するだけに留める
type Task struct {
	db *gorm.DB
}

func NewTask(db *gorm.DB) *Task {
	return &Task{db: db}
}

// TaskFilter はタスク一覧の絞り込み条件
// training-go/gin/internal/repository/task.go の TaskFilterを踏襲しつつ、v2(gRPC)のcursorベースページング用にCursor/UseCursorを追加した
type TaskFilter struct {
	UserID   uint64
	Name     string
	Status   *model.TaskStatus
	LabelIDs []uint64

	// v1(REST)向け: offsetベースのページング + finished_on昇順/降順ソート
	SortFinishedOn string // "asc" / "desc" / "" (createdAt降順)
	Limit          int
	Offset         int

	// v2(gRPC)向け: keyset(cursor)ベースのページング
	// 学習目的の単純化として、cursorページングは常に id の昇順固定とする
	// (finished_onソート等との併用は本実装のスコープ外。README/コメントに明記)
	UseCursor bool
	Cursor    uint64 // 直前ページの最後の task.ID、0なら先頭から
}

// ListWithoutLabels はTaskのみを取得し、Labelsは一切Preloadしない
// v1(旧実装)がこの結果に対して1件ずつLabelsForTaskを呼ぶことで、
// 意図的にN+1クエリを再現する材料になる
func (r *Task) ListWithoutLabels(ctx context.Context, filter TaskFilter) ([]model.Task, int64, error) {
	db := r.scoped(ctx, filter)

	var total int64
	if err := db.Session(&gorm.Session{}).Model(&model.Task{}).Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("タスク件数取得: %w", err)
	}

	db = r.applySort(db, filter)

	tasks := make([]model.Task, 0)
	if err := db.Limit(filter.Limit).Offset(filter.Offset).Find(&tasks).Error; err != nil {
		return nil, 0, fmt.Errorf("タスク一覧取得(Preloadなし): %w", err)
	}
	return tasks, total, nil
}

// ListWithLabelsPreloaded はPreload("Labels")で一括取得する(N+1解消済み) v2(新実装)が使う
// cursorページング(filter.UseCursor)にも対応する
func (r *Task) ListWithLabelsPreloaded(ctx context.Context, filter TaskFilter) ([]model.Task, error) {
	db := r.scoped(ctx, filter)

	if filter.UseCursor {
		if filter.Cursor > 0 {
			db = db.Where("id > ?", filter.Cursor)
		}
		db = db.Order("id ASC")
	} else {
		db = r.applySort(db, filter)
	}

	tasks := make([]model.Task, 0)
	err := db.
		Preload("Labels").
		Limit(filter.Limit).
		Offset(offsetOrZero(filter)).
		Find(&tasks).Error
	if err != nil {
		return nil, fmt.Errorf("タスク一覧取得(Preloadあり): %w", err)
	}
	return tasks, nil
}

func offsetOrZero(filter TaskFilter) int {
	if filter.UseCursor {
		// keyset pagination では OFFSET を使わない(WHERE id > cursor が代わりを果たす)
		return 0
	}
	return filter.Offset
}

func (r *Task) scoped(ctx context.Context, filter TaskFilter) *gorm.DB {
	db := r.db.WithContext(ctx).Model(&model.Task{}).Where("user_id = ?", filter.UserID)
	if filter.Name != "" {
		db = db.Where("name LIKE ?", "%"+filter.Name+"%")
	}
	if filter.Status != nil {
		db = db.Where("status = ?", *filter.Status)
	}
	if len(filter.LabelIDs) > 0 {
		db = db.Where("id IN (SELECT task_id FROM task_labels WHERE label_id IN ?)", filter.LabelIDs)
	}
	return db
}

func (r *Task) applySort(db *gorm.DB, filter TaskFilter) *gorm.DB {
	if filter.SortFinishedOn != "" {
		if filter.SortFinishedOn == "asc" {
			return db.Order("finished_on ASC")
		}
		return db.Order("finished_on DESC")
	}
	return db.Order("created_at DESC")
}

// LabelsForTask は指定タスク1件分のLabelsだけを取得する
//
// これ自体は普通のクエリだが、v1のservice層がタスク一覧のループの中で
// 「1タスクにつき1回」呼び出すことで、結果として「1回の一覧取得 + N回のラベル取得」
// というN+1クエリを引き起こす(意図的な旧実装、CONTRACT.md参照)
func (r *Task) LabelsForTask(ctx context.Context, taskID uint64) ([]model.Label, error) {
	var labels []model.Label
	err := r.db.WithContext(ctx).
		Joins("JOIN task_labels ON task_labels.label_id = labels.id").
		Where("task_labels.task_id = ?", taskID).
		Find(&labels).Error
	if err != nil {
		return nil, fmt.Errorf("タスク(id=%d)のラベル取得: %w", taskID, err)
	}
	return labels, nil
}

// Get はユーザースコープ付きで1件取得する(他人のタスクは見えない。Rails版の404相当)
func (r *Task) Get(ctx context.Context, id, userID uint64, preloadLabels bool) (*model.Task, error) {
	db := r.db.WithContext(ctx).Where("user_id = ?", userID)
	if preloadLabels {
		db = db.Preload("Labels")
	}
	var task model.Task
	if err := db.First(&task, id).Error; err != nil {
		return nil, err
	}
	return &task, nil
}

// FindMissingLabelIDs は指定したlabelIDsのうち、labelsテーブルに実在しないものを返す
// (空スライスなら全て実在する)
//
// 【2回目のテスト監査で発覚】task_labelsテーブルにはlabel_id/task_idへの外部キー制約が
// 無く(migrations/000004参照)、Task.Create/Updateも存在しないlabel_idを一切検証せずに
// Association.Replaceへ渡していたため、存在しないlabel_idを指定してもエラーにならず、
// task_labelsに永久に孤立する行(id 999999999のようなラベルを指すが、labels側には
// 対応する行が無い)が作られてしまっていた(実際に統合テストで再現・確認した実バグ)
//
// サービス層(service.TaskService.Create/Update)でこのメソッドの結果を見て
// ErrValidationを返すことで、この不整合データの発生を防ぐ
func (r *Task) FindMissingLabelIDs(ctx context.Context, labelIDs []uint64) ([]uint64, error) {
	if len(labelIDs) == 0 {
		return nil, nil
	}
	var existing []uint64
	if err := r.db.WithContext(ctx).Model(&model.Label{}).Where("id IN ?", labelIDs).Pluck("id", &existing).Error; err != nil {
		return nil, fmt.Errorf("label_idの存在確認: %w", err)
	}
	existingSet := make(map[uint64]bool, len(existing))
	for _, id := range existing {
		existingSet[id] = true
	}
	var missing []uint64
	seen := make(map[uint64]bool, len(labelIDs))
	for _, id := range labelIDs {
		if seen[id] {
			continue
		}
		seen[id] = true
		if !existingSet[id] {
			missing = append(missing, id)
		}
	}
	return missing, nil
}

// Create はタスクを作成し、ラベルの関連付けも同時に行う
func (r *Task) Create(ctx context.Context, task *model.Task, labelIDs []uint64) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(task).Error; err != nil {
			return fmt.Errorf("タスク作成: %w", err)
		}
		if len(labelIDs) > 0 {
			if err := r.replaceLabels(tx, task, labelIDs); err != nil {
				return err
			}
		}
		return nil
	})
}

// Update はタスク本体とラベルの関連付けを更新する
func (r *Task) Update(ctx context.Context, task *model.Task, labelIDs []uint64) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Save(task).Error; err != nil {
			return fmt.Errorf("タスク更新: %w", err)
		}
		if err := r.replaceLabels(tx, task, labelIDs); err != nil {
			return err
		}
		return nil
	})
}

func (r *Task) replaceLabels(tx *gorm.DB, task *model.Task, labelIDs []uint64) error {
	// 【テスト監査で発見・修正した実バグ】重複するlabel_id(例: [3,3,5])をそのまま
	// GORMのAssociation.Replaceへ渡すと、同じ主キー(label_id=3)のLabel構造体が
	// 2つ含まれることになり、GORMが内部的に生成するUPSERT/UPDATE文でauto-increment値が
	// 競合し「Error 1869: Auto-increment value in UPDATE conflicts with internally
	// generated values」という生のDBエラーになる(task_labelsの(task_id,label_id)への
	// UNIQUE制約(migrations/000004)以前に、GORM自身の内部処理で落ちる)。
	// 呼び出し元(bff/フロント)が同じラベルを重複指定することはUIの都合上まず無いが、
	// 直接APIを叩く場合には普通に起こりうる入力のため、ここで先に重複除去しておく
	seen := make(map[uint64]struct{}, len(labelIDs))
	labels := make([]model.Label, 0, len(labelIDs))
	for _, id := range labelIDs {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		labels = append(labels, model.Label{ID: id})
	}
	// 【実機検証で判明した不具合】Omit("Labels.*")を付けないと、GORMはIDしか
	// 埋まっていないこのLabel構造体を「labelsテーブル自体に保存すべき値」として
	// 扱い、labels行へのINSERTを試みてしまう(created_atがNOT NULLのため
	// "Field 'created_at' doesn't have a default value" で失敗する)
	// ここでは既存のlabel行をIDで参照してtask_labels(中間テーブル)だけを
	// 付け替えたいので、Omit("Labels.*")でlabels自体の保存を明示的に抑止する
	if err := tx.Model(task).Omit("Labels.*").Association("Labels").Replace(labels); err != nil {
		return fmt.Errorf("タスク(id=%d)のラベル付け替え: %w", task.ID, err)
	}
	return nil
}

// Delete はユーザースコープ付きで削除する
//
// 【3回目のテスト監査(認可・データ整合性観点)で発覚】task_labelsにFK制約が無いため
// (CountTaskLabelsのコメント参照)、Task削除時にtask_labels側の紐付け行を消していなかった
//
// AUTO_INCREMENTのidは再利用されないため他タスクとの衝突は起きず実害は乏しいが、
// 「そのタスクにしか使われていないラベルは、タスク削除後は削除できるはず」という
// LabelService.Deleteの利用中判定(CountTaskLabels)と矛盾する挙動になっていたため、
// タスク削除と同じトランザクションでtask_labelsの関連行も削除するよう修正する
func (r *Task) Delete(ctx context.Context, id, userID uint64) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Where("user_id = ?", userID).Delete(&model.Task{}, id)
		if result.Error != nil {
			return fmt.Errorf("タスク削除: %w", result.Error)
		}
		if result.RowsAffected == 0 {
			return gorm.ErrRecordNotFound
		}
		if err := tx.Exec("DELETE FROM task_labels WHERE task_id = ?", id).Error; err != nil {
			return fmt.Errorf("タスク削除に伴うtask_labels後始末: %w", err)
		}
		return nil
	})
}

// --- 以下、CONTRACT.mdセクション11(BFF非経由の外部公開API)専用のメソッド ---
//
// 既存のTaskFilter(UseCursor/Cursor)はgRPC v2向けの「idのみの単純cursor」だが、
// 外部APIでは学習効果を広げるため、あえて別の(created_at, id)複合キーによる keysetページングを持つ
// REST v1のTaskFilter/offsetとも意図的に分離し、
// 「offsetとcursorの性能特性の対比」という外部APIの主題に絞っている

// ListOffsetForExternalAPI はoffsetベースのページング(外部API v1、Feature Flag OFF)
// created_at DESC, id DESC で安定ソートする
//
// 【性能上の弱点】OFFSETはDBが「読み飛ばす行」も含めてスキャンするため、page が大きくなるほど(例: page=10000)応答が線形に遅くなる
// 少量データの学習環境では体感できないが、実務でよくある「管理画面の一覧が後半のページだけ遅い」問題の典型例
func (r *Task) ListOffsetForExternalAPI(ctx context.Context, userID uint64, page, pageSize int) ([]model.Task, int64, error) {
	db := r.db.WithContext(ctx).Model(&model.Task{}).Where("user_id = ?", userID)

	var total int64
	if err := db.Session(&gorm.Session{}).Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("タスク件数取得(外部API v1): %w", err)
	}

	offset := (page - 1) * pageSize
	tasks := make([]model.Task, 0)
	err := db.
		Preload("Labels").
		Order("created_at DESC, id DESC").
		Limit(pageSize).
		Offset(offset).
		Find(&tasks).Error
	if err != nil {
		return nil, 0, fmt.Errorf("タスク一覧取得(外部API v1): %w", err)
	}
	return tasks, total, nil
}

// ListCursorForExternalAPI はkeyset(cursor)ベースのページング(外部API v2、Feature Flag ON) cursorAfterがnilなら先頭ページ
// (created_at, id)の複合条件でOFFSETを使わずに「前ページの最後の行より後ろ」を絞り込むため、pageが深くなっても性能が劣化しない
func (r *Task) ListCursorForExternalAPI(ctx context.Context, userID uint64, cursorAfter *TaskCursor, limit int) ([]model.Task, error) {
	db := r.db.WithContext(ctx).Model(&model.Task{}).Where("user_id = ?", userID)

	if cursorAfter != nil {
		// created_at DESC, id DESC の並びで「cursorより後ろ(より古い/より小さいid)」を取る
		// created_atが同値のtie-breakをidで行うのがkeysetページングの定石
		db = db.Where(
			"(created_at < ?) OR (created_at = ? AND id < ?)",
			cursorAfter.CreatedAt, cursorAfter.CreatedAt, cursorAfter.ID,
		)
	}

	tasks := make([]model.Task, 0)
	err := db.
		Preload("Labels").
		Order("created_at DESC, id DESC").
		Limit(limit).
		Find(&tasks).Error
	if err != nil {
		return nil, fmt.Errorf("タスク一覧取得(外部API v2): %w", err)
	}
	return tasks, nil
}

// TaskCursor は外部API v2のcursorが指し示す「直前ページの最後の行」の位置
type TaskCursor struct {
	CreatedAt time.Time
	ID        uint64
}
