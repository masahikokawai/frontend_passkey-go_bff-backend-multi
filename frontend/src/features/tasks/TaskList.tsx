import { useCallback, useEffect, useRef, useState } from "react";
import { apiFetch } from "@/shared/api/client";
import { createTask, deleteTask, fetchTasks, updateTask } from "./api";
import TaskDeleteButton from "./TaskDeleteButton";
import TaskForm from "./TaskForm";
import type { Label, Task } from "./types";

interface LabelListResponse {
  labels: Label[];
}

// タスク一覧(新実装、TypeScript)
// Rails: TasksController#index に相当
//
// Feature Flag "frontend.tasks-ts-rewrite" がONのときに TaskListSwitch から表示される
// 旧実装は ./legacy/TaskList.jsx (JavaScript、一覧表示のみの縮小版)
export default function TaskList() {
  const [tasks, setTasks] = useState<Task[]>([]);
  const [labelOptions, setLabelOptions] = useState<Label[]>([]);
  const [loading, setLoading] = useState(true);
  const [editing, setEditing] = useState<Task | null>(null);
  // 作成/更新の成功メッセージはTaskForm自身が表示する
  // 削除だけはTaskForm経由しないため、ここで持つ(CONTRACT.mdセクション15)
  const [deleteMessage, setDeleteMessage] = useState<string | null>(null);
  // 削除失敗時のエラー表示(以前はここにtry/catchが無く、削除APIが失敗すると
  // 未処理のPromise rejectionになりユーザーには何も表示されなかった)
  const [deleteError, setDeleteError] = useState<string | null>(null);
  // 一覧取得(reload)自体の失敗時のエラー表示(同上の理由
  // これが無いと通信失敗時に「読み込み中...」のまま画面が固まり、未処理のPromise rejectionにもなっていた)
  const [loadError, setLoadError] = useState<string | null>(null);

  useEffect(() => {
    if (!deleteMessage) return undefined;
    const timer = setTimeout(() => setDeleteMessage(null), 3000);
    return () => clearTimeout(timer);
  }, [deleteMessage]);

  // 【テスト監査で発見・修正】reload()は削除・作成・更新の各成功時にも呼ばれるため、
  // ユーザーが立て続けに2つの操作を行うと2つのreload()が同時に実行され得る
  // (例: 行Aの削除→即座に行Bの編集を送信、の2アクション)。fetchTasksの応答が
  // リクエストを送った順番通りに返ってくる保証は無いため、後に呼ばれたreload()の
  // 応答が先に、先に呼ばれたreload()の応答が後に届くと、新しい一覧が古い一覧で
  // 上書きされてしまう(表示上は何もエラーが出ないまま、直前の操作結果が消える)
  //
  // reload()呼び出しごとに採番し、応答が返ってきた時点で「自分が最新のreload()か」を
  // 確認することで、古い応答を無視する(最後にsetTasksするのは常に最後に呼ばれたreload())
  const latestRequestIdRef = useRef(0);
  const reload = useCallback(async () => {
    const requestId = ++latestRequestIdRef.current;
    setLoading(true);
    try {
      const res = await fetchTasks();
      if (requestId !== latestRequestIdRef.current) return;
      setTasks(res.tasks);
      setLoadError(null);
    } catch (err) {
      if (requestId !== latestRequestIdRef.current) return;
      setLoadError(err instanceof Error ? err.message : "一覧の取得に失敗しました");
    } finally {
      if (requestId === latestRequestIdRef.current) setLoading(false);
    }
  }, []);

  useEffect(() => {
    reload();
    apiFetch<LabelListResponse>("/api/labels").then((res) => setLabelOptions(res.labels));
  }, [reload]);

  if (loading) {
    return <div>読み込み中...</div>;
  }

  return (
    <div data-testid="task-list">
      <h1>タスク一覧(新実装 / TypeScript)</h1>

      {loadError && (
        <div className="alert alert-danger" role="alert" data-testid="load-error-message">
          {loadError}
        </div>
      )}
      {deleteMessage && (
        <div className="alert alert-success" role="alert" data-testid="success-message">
          {deleteMessage}
        </div>
      )}
      {deleteError && (
        <div className="alert alert-danger" role="alert" data-testid="delete-error-message">
          {deleteError}
        </div>
      )}

      <TaskForm
        key={editing?.id ?? "new"}
        initial={editing ?? undefined}
        labelOptions={labelOptions}
        submitLabel={editing ? "更新" : "作成"}
        onSubmit={async (input) => {
          if (editing) {
            await updateTask(editing.id, input);
            setEditing(null);
          } else {
            await createTask(input);
          }
          await reload();
        }}
      />

      <table className="table">
        <thead>
          <tr>
            <th>タスク名</th>
            <th>ステータス</th>
            <th>期限</th>
            <th>ラベル</th>
            <th />
          </tr>
        </thead>
        <tbody>
          {tasks.map((task) => (
            <tr key={task.id} data-testid="task-row" data-task-name={task.name}>
              <td>{task.name}</td>
              <td>{task.status}</td>
              <td>{task.finishedOn}</td>
              {/* 【テスト監査で発見・修正】backendが5言語実装のいずれかでlabelsをnullとして
                  返す不具合があった場合でも、一覧全体が壊れず1行だけ空表示になるようにする */}
              <td>{(task.labels ?? []).map((l) => l.name).join(", ")}</td>
              <td>
                <button
                  type="button"
                  className="btn btn-sm btn-outline-secondary me-2"
                  onClick={() => setEditing(task)}
                  data-testid="task-edit-button"
                >
                  編集
                </button>
                <TaskDeleteButton
                  onDelete={async () => {
                    setDeleteError(null);
                    try {
                      await deleteTask(task.id);
                      if (editing?.id === task.id) setEditing(null);
                      setDeleteMessage("タスクを削除しました");
                      await reload();
                    } catch (err) {
                      setDeleteError(err instanceof Error ? err.message : "削除に失敗しました");
                    }
                  }}
                />
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
