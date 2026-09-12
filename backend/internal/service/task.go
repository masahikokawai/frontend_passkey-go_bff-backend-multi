// Package service はビジネスロジック層
// http→handler→service→repository→db という
// 層構造のうち、バリデーション・トランザクション境界・ドメインルールを持つ
package service

import (
	"context"
	"fmt"
	"time"

	"github.com/masahikokawai/training-go/bff-gin/backend/internal/model"
	"github.com/masahikokawai/training-go/bff-gin/backend/internal/repository"
)

// TaskService はTask関連のユースケースを提供する
//
// List系だけ v1(N+1, task_v1.go) / v2(Preload+cursor, task_v2.go) に分けているのは、
// CONTRACT.mdで定義したBFFレベルのStrangler Fig学習デモ(N+1解消のビフォーアフター比較)
// のためであり、Create/Update/Delete/Getは業務ロジックとして両者で変える理由が無いため
// 共通実装をこのファイルにまとめている(不要な重複を避けるための判断)
type TaskService struct {
	repo *repository.Task
}

func NewTaskService(repo *repository.Task) *TaskService {
	return &TaskService{repo: repo}
}

// LabelDTO / TaskDTO はAPI層(REST v1ハンドラ・gRPC v2サービス)へ渡す共通の内部表現
// BFFはこの形をさらにReact向けJSON(CONTRACT.md セクション3)へ変換する
type LabelDTO struct {
	ID   uint64
	Name string
}

type TaskDTO struct {
	ID          uint64
	Name        string
	Description *string
	Status      string
	FinishedOn  time.Time
	Labels      []LabelDTO
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// TaskInput はCreate/Updateの入力
// Rails対比: strong parameters(`task_params`)に相当する、外部入力の受け皿
type TaskInput struct {
	Name        string
	Description *string
	Status      string
	FinishedOn  time.Time
	LabelIDs    []uint64
}

// validate はRails版のバリデーション(Task モデルの validates 宣言群)を再現する
//   - name: 必須・20文字以内
//   - finished_on: 必須・過去日不可
//   - status: enumの範囲内
func validateTaskInput(in TaskInput) error {
	if in.Name == "" {
		return fmt.Errorf("%w: nameは必須です", ErrValidation)
	}
	if len([]rune(in.Name)) > 20 {
		return fmt.Errorf("%w: nameは20文字以内である必要があります", ErrValidation)
	}
	if in.FinishedOn.Before(today()) {
		return fmt.Errorf("%w: finished_onに過去日は指定できません", ErrValidation)
	}
	if _, err := model.TaskStatusFromString(in.Status); err != nil {
		return fmt.Errorf("%w: %v", ErrValidation, err)
	}
	return nil
}

// today はfinished_onの過去日判定の基準(時刻を切り捨てた「今日」)
// time.Now()を関数の外に切り出すことで、将来テスト時にモック可能にしている
// (Goには「現在時刻を差し替える」言語機能が無いため、依存として注入できる形にするのが定石)
//
// 【テスト監査で発見・修正】以前はnow.Location()(サーバープロセスのtime.Local、
// 実行環境のTZ次第で不定)を基準にしていたが、finished_on自体はハンドラ側で
// time.Parse("2006-01-02", ...)によりUTC基準でパースされている(Goのtime.Parseは
// タイムゾーン情報が無い書式の場合UTCとして解釈する仕様のため)。
// 「比較対象の一方はUTC・もう一方はサーバーのローカルタイムゾーン」という不整合があり、
// サーバーのタイムゾーンがUTCから離れるほど(例: JST=UTC+9)、本来「今日」であるはずの
// finished_onが「過去日」と誤判定される時間帯が生じていた。
// さらにこの不整合はGo単体の問題に留まらず、Rust/Rails(いずれも明示的にUTC基準)と
// Scala両実装(JVMのデフォルトタイムゾーン依存で同じ不定さを持つ)との間でも、
// 同一リクエスト・同一時刻に対して受理/拒否の判定が割れる契約違反(CONTRACT.mdセクション20.5)を
// 引き起こしていた。UTC固定にすることでfinished_onのパース基準と一致させ、かつ
// Rust/Railsと同じ基準に揃える
func today() time.Time {
	now := time.Now().UTC()
	return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
}

func toTaskDTO(t model.Task) TaskDTO {
	labels := make([]LabelDTO, 0, len(t.Labels))
	for _, l := range t.Labels {
		labels = append(labels, LabelDTO{ID: l.ID, Name: l.Name})
	}
	return TaskDTO{
		ID:          t.ID,
		Name:        t.Name,
		Description: t.Description,
		Status:      t.Status.String(),
		FinishedOn:  t.FinishedOn,
		Labels:      labels,
		CreatedAt:   t.CreatedAt,
		UpdatedAt:   t.UpdatedAt,
	}
}

// Get は1件取得(常にPreloadする
// 1件だけならN+1にならないため使い分ける意味が無い)
func (s *TaskService) Get(ctx context.Context, id, userID uint64) (TaskDTO, error) {
	task, err := s.repo.Get(ctx, id, userID, true)
	if err != nil {
		return TaskDTO{}, fmt.Errorf("%w", ErrNotFound)
	}
	return toTaskDTO(*task), nil
}

// validateLabelIDsExist はlabel_idsが全てlabelsテーブルに実在することを確認する
// (見つかったバグ: task_labelsにFK制約が無いため、この確認をしないと存在しない
// label_idを指定してもエラーにならず孤立データが作られてしまう
// repository.FindMissingLabelIDs参照)
//
// 呼び出しと実際のCreate/Updateの間にラベル削除が割り込むごく僅かな競合(TOCTOU)は
// 理論上あり得るが、labelsは管理者が低頻度で操作するデータであり実害が乏しいため、
// このチェックをトランザクションの外に置くシンプルな実装を許容している
func (s *TaskService) validateLabelIDsExist(ctx context.Context, labelIDs []uint64) error {
	missing, err := s.repo.FindMissingLabelIDs(ctx, labelIDs)
	if err != nil {
		return err
	}
	if len(missing) > 0 {
		return fmt.Errorf("%w: 存在しないlabel_idが指定されました: %v", ErrValidation, missing)
	}
	return nil
}

// Create はタスクを新規作成する
func (s *TaskService) Create(ctx context.Context, userID uint64, in TaskInput) (TaskDTO, error) {
	if err := validateTaskInput(in); err != nil {
		return TaskDTO{}, err
	}
	if err := s.validateLabelIDsExist(ctx, in.LabelIDs); err != nil {
		return TaskDTO{}, err
	}
	status, _ := model.TaskStatusFromString(in.Status)
	task := &model.Task{
		Name:        in.Name,
		Description: in.Description,
		Status:      status,
		FinishedOn:  in.FinishedOn,
		UserID:      userID,
	}
	if err := s.repo.Create(ctx, task, in.LabelIDs); err != nil {
		return TaskDTO{}, fmt.Errorf("タスク作成: %w", err)
	}
	return s.Get(ctx, task.ID, userID)
}

// Update は既存タスクを更新する(ユーザースコープ付き)
func (s *TaskService) Update(ctx context.Context, id, userID uint64, in TaskInput) (TaskDTO, error) {
	if err := validateTaskInput(in); err != nil {
		return TaskDTO{}, err
	}
	if err := s.validateLabelIDsExist(ctx, in.LabelIDs); err != nil {
		return TaskDTO{}, err
	}
	existing, err := s.repo.Get(ctx, id, userID, false)
	if err != nil {
		return TaskDTO{}, fmt.Errorf("%w", ErrNotFound)
	}
	status, _ := model.TaskStatusFromString(in.Status)
	existing.Name = in.Name
	existing.Description = in.Description
	existing.Status = status
	existing.FinishedOn = in.FinishedOn
	if err := s.repo.Update(ctx, existing, in.LabelIDs); err != nil {
		return TaskDTO{}, fmt.Errorf("タスク更新: %w", err)
	}
	return s.Get(ctx, id, userID)
}

// Delete はタスクを削除する(ユーザースコープ付き)
func (s *TaskService) Delete(ctx context.Context, id, userID uint64) error {
	if err := s.repo.Delete(ctx, id, userID); err != nil {
		return fmt.Errorf("%w", ErrNotFound)
	}
	return nil
}
