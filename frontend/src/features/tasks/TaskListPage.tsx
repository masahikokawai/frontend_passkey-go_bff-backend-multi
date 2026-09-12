import { useCallback, useEffect, useRef, useState } from "react";
import { deleteTask, fetchTasks } from "./api";
import TaskDeleteButton from "./TaskDeleteButton";
import type { Task } from "./types";

// タスク一覧(別ページ遷移版UX)
// CONTRACT.mdセクション19:
// frontend.task-create-ux="page" のときに TaskCreateUXSwitch から表示される
//
// 登録・編集は別ルート(/tasks/new, /tasks/:id/edit、TaskFormPage.tsx)に分離している
// (常設フォーム版・モーダル版とは違い、フォームをこのコンポーネント自身は持たない)
export default function TaskListPage() {
  const [tasks, setTasks] = useState<Task[]>([]);
  const [loading, setLoading] = useState(true);
  const [deleteMessage, setDeleteMessage] = useState<string | null>(null);
  const [deleteError, setDeleteError] = useState<string | null>(null);
  const [loadError, setLoadError] = useState<string | null>(null);

  useEffect(() => {
    if (!deleteMessage) return undefined;
    const timer = setTimeout(() => setDeleteMessage(null), 3000);
    return () => clearTimeout(timer);
  }, [deleteMessage]);

  // 【テスト監査で発見・修正、TaskList.tsxと同じ理由】reload()呼び出しごとに採番し、
  // 応答が返ってきた時点で「自分が最新のreload()か」を確認することで、
  // 2つのreload()が重なった際に古い応答が新しい一覧を上書きしてしまうのを防ぐ
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
  }, [reload]);

  if (loading) {
    return <div>読み込み中...</div>;
  }

  return (
    <div data-testid="task-list">
      <h1>タスク一覧(別ページ遷移版UX)</h1>

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

      <a href="/tasks/new" className="btn btn-primary mb-3" data-testid="task-create-link">
        タスクを登録
      </a>

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
              <td>{(task.labels ?? []).map((l) => l.name).join(", ")}</td>
              <td>
                <a
                  href={`/tasks/${task.id}/edit`}
                  className="btn btn-sm btn-outline-secondary me-2"
                  data-testid="task-edit-button"
                >
                  編集
                </a>
                <TaskDeleteButton
                  onDelete={async () => {
                    setDeleteError(null);
                    try {
                      await deleteTask(task.id);
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
