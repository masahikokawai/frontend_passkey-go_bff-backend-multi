import { useCallback, useEffect, useRef, useState } from "react";
import { apiFetch } from "@/shared/api/client";
import { createTask, deleteTask, fetchTasks, updateTask } from "../api";
import TaskDeleteButton from "../TaskDeleteButton";
import TaskForm from "./TaskForm";

// タスク一覧(旧実装/JavaScript、モーダル版UX)
// CONTRACT.mdセクション19.7:
// frontend.task-create-ux="modal" のときに legacy/TaskCreateUXSwitch から表示される
//
// TaskListModal.tsx(新実装)と対称的な構成
// フォーム自体(legacy/TaskForm.jsx)・APIロジックは legacy/TaskList.jsx(inline版)と共通で、
// UIの入れ物(常設 or モーダル)だけが異なる
export default function TaskListModal() {
  const [tasks, setTasks] = useState([]);
  const [labelOptions, setLabelOptions] = useState([]);
  const [loading, setLoading] = useState(true);
  const [editing, setEditing] = useState(null);
  const [showModal, setShowModal] = useState(false);
  const [saveMessage, setSaveMessage] = useState(null);
  const [deleteMessage, setDeleteMessage] = useState(null);
  const [deleteError, setDeleteError] = useState(null);
  const [loadError, setLoadError] = useState(null);

  useEffect(() => {
    if (!deleteMessage && !saveMessage) return undefined;
    const timer = setTimeout(() => {
      setDeleteMessage(null);
      setSaveMessage(null);
    }, 3000);
    return () => clearTimeout(timer);
  }, [deleteMessage, saveMessage]);

  // 【テスト監査で発見・修正】TaskListModal.tsx(新実装)と同じstale-response上書きバグ
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
    apiFetch("/api/labels").then((res) => setLabelOptions(res.labels));
  }, [reload]);

  // 【テスト監査で発見・修正】TaskListModal.tsx(新実装)と同じ理由(モーダルが素のdivで
  // 開閉時のフォーカス管理が一切無かった)で、キーボードのみで操作するユーザーが
  // 閉じた後に操作を続けられなくなる問題があった
  const previousFocusRef = useRef(null);
  const modalRef = useRef(null);

  function openCreate() {
    previousFocusRef.current = document.activeElement;
    setEditing(null);
    setShowModal(true);
  }

  function openEdit(task) {
    previousFocusRef.current = document.activeElement;
    setEditing(task);
    setShowModal(true);
  }

  function closeModal() {
    setShowModal(false);
    setEditing(null);
    previousFocusRef.current?.focus();
  }

  useEffect(() => {
    if (showModal) modalRef.current?.focus();
  }, [showModal]);

  function handleModalKeyDown(event) {
    if (event.key === "Escape") {
      closeModal();
      return;
    }
    if (event.key !== "Tab" || !modalRef.current) return;
    const focusable = modalRef.current.querySelectorAll(
      'button, [href], input, select, textarea, [tabindex]:not([tabindex="-1"])'
    );
    if (focusable.length === 0) return;
    const first = focusable[0];
    const last = focusable[focusable.length - 1];
    if (event.shiftKey && document.activeElement === first) {
      event.preventDefault();
      last.focus();
    } else if (!event.shiftKey && document.activeElement === last) {
      event.preventDefault();
      first.focus();
    }
  }

  if (loading) {
    return <div>読み込み中...</div>;
  }

  return (
    <div data-testid="task-list">
      <h1>タスク一覧(旧実装 / JavaScript、モーダル版UX)</h1>

      {loadError && (
        <div className="alert alert-danger" role="alert" data-testid="load-error-message">
          {loadError}
        </div>
      )}
      {saveMessage && (
        <div className="alert alert-success" role="alert" data-testid="success-message">
          {saveMessage}
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

      <button
        type="button"
        className="btn btn-primary mb-3"
        onClick={openCreate}
        data-testid="task-create-button"
      >
        タスクを登録
      </button>

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
                <button
                  type="button"
                  className="btn btn-sm btn-outline-secondary me-2"
                  onClick={() => openEdit(task)}
                  data-testid="task-edit-button"
                >
                  編集
                </button>
                <TaskDeleteButton
                  onDelete={async () => {
                    setDeleteError(null);
                    try {
                      await deleteTask(task.id);
                      if (editing?.id === task.id) closeModal();
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

      {showModal && (
        <div
          ref={modalRef}
          className="modal show d-block"
          tabIndex={-1}
          role="dialog"
          aria-modal="true"
          aria-labelledby="task-modal-title"
          onKeyDown={handleModalKeyDown}
          data-testid="task-create-modal"
          style={{ backgroundColor: "rgba(0,0,0,0.5)" }}
        >
          <div className="modal-dialog" role="document">
            <div className="modal-content">
              <div className="modal-header">
                <h5 className="modal-title" id="task-modal-title">
                  {editing ? "タスクを編集" : "タスクを登録"}
                </h5>
                <button
                  type="button"
                  className="close"
                  onClick={closeModal}
                  data-testid="task-modal-close"
                  aria-label="閉じる"
                >
                  <span aria-hidden="true">&times;</span>
                </button>
              </div>
              <div className="modal-body">
                <TaskForm
                  key={editing?.id ?? "new"}
                  initial={editing ?? undefined}
                  labelOptions={labelOptions}
                  submitLabel={editing ? "更新" : "作成"}
                  onSubmit={async (input) => {
                    if (editing) {
                      await updateTask(editing.id, input);
                      setSaveMessage("タスクを更新しました");
                    } else {
                      await createTask(input);
                      setSaveMessage("タスクを作成しました");
                    }
                    await reload();
                    closeModal();
                  }}
                />
              </div>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
