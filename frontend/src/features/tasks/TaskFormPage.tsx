import { useEffect, useState } from "react";
import { useNavigate, useParams } from "react-router-dom";
import { apiFetch } from "@/shared/api/client";
import { createTask, fetchTask, updateTask } from "./api";
import TaskForm from "./TaskForm";
import type { Label, Task } from "./types";

interface LabelListResponse {
  labels: Label[];
}

interface TaskFormPageProps {
  mode: "create" | "edit";
}

// タスク登録/編集の専用ページ(CONTRACT.mdセクション19: frontend.task-create-ux="page"のときに使う経路)
// /tasks/new(作成)・/tasks/:id/edit(編集)の両方をこの1コンポーネントで扱う
//
// 【重要】
// このページには必ず一覧へ戻るリンクを設置
// 直近「リンク/ボタンから遷移した画面に戻る手段が無い」というバグ(admin/rails、存在しないIDアクセス時の生の例外ページ)を発見
// 同じ轍を踏まないための明示的な考慮
export default function TaskFormPage({ mode }: TaskFormPageProps) {
  const { id } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const [task, setTask] = useState<Task | undefined>(undefined);
  const [labelOptions, setLabelOptions] = useState<Label[]>([]);
  const [loading, setLoading] = useState(mode === "edit");
  const [loadError, setLoadError] = useState<string | null>(null);

  useEffect(() => {
    apiFetch<LabelListResponse>("/api/labels").then((res) => setLabelOptions(res.labels));
  }, []);

  useEffect(() => {
    if (mode !== "edit" || !id) return;
    setLoading(true);
    fetchTask(Number(id))
      .then((t) => {
        setTask(t);
        setLoadError(null);
      })
      .catch((err) => {
        setLoadError(err instanceof Error ? err.message : "タスクの取得に失敗しました");
      })
      .finally(() => setLoading(false));
  }, [mode, id]);

  return (
    <div data-testid="task-form-page">
      <h1>{mode === "edit" ? "タスクを編集" : "タスクを登録"}</h1>

      <a href="/tasks" className="btn btn-outline-secondary btn-sm mb-3" data-testid="back-to-tasks-link">
        ← タスク一覧に戻る
      </a>

      {loadError && (
        <div className="alert alert-danger" role="alert" data-testid="load-error-message">
          {loadError}
        </div>
      )}

      {loading ? (
        <div>読み込み中...</div>
      ) : (
        (mode === "create" || task) && (
          <TaskForm
            initial={task}
            labelOptions={labelOptions}
            submitLabel={mode === "edit" ? "更新" : "作成"}
            onSubmit={async (input) => {
              if (mode === "edit" && id) {
                await updateTask(Number(id), input);
              } else {
                await createTask(input);
              }
              navigate("/tasks");
            }}
          />
        )
      )}
    </div>
  );
}
