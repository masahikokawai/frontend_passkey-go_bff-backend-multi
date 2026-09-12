import { useEffect, useState } from "react";
import { useNavigate, useParams } from "react-router-dom";
import { apiFetch } from "@/shared/api/client";
import { createTask, fetchTask, updateTask } from "../api";
import TaskForm from "./TaskForm";

// タスク登録/編集の専用ページ(旧実装/JavaScript)
// CONTRACT.mdセクション19.7:
// frontend.task-create-ux="page" のときに使う経路
// /tasks/new(作成)・/tasks/:id/edit(編集)の両方をこの1コンポーネントで扱う
// TaskFormPage.tsx(新実装)と対称的な構成
//
// 【重要】
// TaskFormPage.tsxと同じ理由により、必ず一覧へ戻るリンクを設置する
// 「リンク/ボタンから遷移した画面に戻る手段が無い」というバグを修正
export default function TaskFormPage({ mode }) {
  const { id } = useParams();
  const navigate = useNavigate();
  const [task, setTask] = useState(undefined);
  const [labelOptions, setLabelOptions] = useState([]);
  const [loading, setLoading] = useState(mode === "edit");
  const [loadError, setLoadError] = useState(null);

  useEffect(() => {
    apiFetch("/api/labels").then((res) => setLabelOptions(res.labels));
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
