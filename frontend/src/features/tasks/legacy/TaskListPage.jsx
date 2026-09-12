import { useCallback, useEffect, useRef, useState } from "react";
import { deleteTask, fetchTasks } from "../api";
import TaskDeleteButton from "../TaskDeleteButton";

// タスク一覧(旧実装/JavaScript、別ページ遷移版UX)
// CONTRACT.mdセクション19.7:
// frontend.task-create-ux="page" のときに legacy/TaskCreateUXSwitch から表示される
//
// TaskListPage.tsx(新実装)と対称的な構成
// 登録・編集は別ルート
// (/tasks/new, /tasks/:id/edit、legacy/TaskFormPage.jsx)に分離している
export default function TaskListPage() {
  const [tasks, setTasks] = useState([]);
  const [loading, setLoading] = useState(true);
  const [deleteMessage, setDeleteMessage] = useState(null);
  const [deleteError, setDeleteError] = useState(null);
  const [loadError, setLoadError] = useState(null);

  useEffect(() => {
    if (!deleteMessage) return undefined;
    const timer = setTimeout(() => setDeleteMessage(null), 3000);
    return () => clearTimeout(timer);
  }, [deleteMessage]);

  // 【テスト監査で発見・修正】TaskListPage.tsx(新実装)と同じstale-response上書きバグ
  // (legacy/TaskList.jsxの同名コメント参照)がここにもあったため、同じ採番ガードで修正する
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
      <h1>タスク一覧(旧実装 / JavaScript、別ページ遷移版UX)</h1>

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
