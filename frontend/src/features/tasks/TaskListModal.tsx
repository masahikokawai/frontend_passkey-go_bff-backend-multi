import { useCallback, useEffect, useRef, useState } from "react";
import { apiFetch } from "@/shared/api/client";
import { createTask, deleteTask, fetchTasks, updateTask } from "./api";
import TaskDeleteButton from "./TaskDeleteButton";
import TaskForm from "./TaskForm";
import type { Label, Task } from "./types";

interface LabelListResponse {
  labels: Label[];
}

// タスク一覧(モーダル版UX)
// CONTRACT.mdセクション19:
// frontend.task-create-ux="modal" のときに TaskCreateUXSwitch から表示される
//
// 常設フォーム版(TaskList.tsx)との違いは「登録/編集フォームをモーダルへ格納する」点のみ
// フォーム自体(TaskForm.tsx)・APIロジックはTaskList.tsxと共通で、UIの入れ物(常設 or モーダル)だけが異なる
export default function TaskListModal() {
  const [tasks, setTasks] = useState<Task[]>([]);
  const [labelOptions, setLabelOptions] = useState<Label[]>([]);
  const [loading, setLoading] = useState(true);
  const [editing, setEditing] = useState<Task | null>(null);
  const [showModal, setShowModal] = useState(false);
  // 作成/更新成功時のメッセージ
  // モーダル版はフォームが閉じてしまうため、TaskForm自身の
  // 成功メッセージ(3秒で消える)ではユーザーが見る間もなく消えてしまう
  // そのため一覧側(削除成功時のdeleteMessageと同じ仕組み)で表示する
  const [saveMessage, setSaveMessage] = useState<string | null>(null);
  const [deleteMessage, setDeleteMessage] = useState<string | null>(null);
  const [deleteError, setDeleteError] = useState<string | null>(null);
  const [loadError, setLoadError] = useState<string | null>(null);

  useEffect(() => {
    if (!deleteMessage && !saveMessage) return undefined;
    const timer = setTimeout(() => {
      setDeleteMessage(null);
      setSaveMessage(null);
    }, 3000);
    return () => clearTimeout(timer);
  }, [deleteMessage, saveMessage]);

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
    apiFetch<LabelListResponse>("/api/labels").then((res) => setLabelOptions(res.labels));
  }, [reload]);

  // 【テスト監査で発見・修正】モーダルはBootstrapのJSプラグインに頼らない素のdivのため、
  // 開閉時のフォーカス管理が一切無かった。開いた瞬間にフォーカスがどこにも移らず、
  // 閉じた後もフォーカスが失われた要素(モーダル内の要素)に残ったまま(=以後Tabキーが
  // 効かなくなる)キーボードのみで操作するユーザーにとって致命的な状態だった
  // モーダルを開いた時点の要素を記憶し、閉じたらそこへフォーカスを戻す
  const previousFocusRef = useRef<HTMLElement | null>(null);
  const modalRef = useRef<HTMLDivElement>(null);

  function openCreate() {
    previousFocusRef.current = document.activeElement as HTMLElement;
    setEditing(null);
    setShowModal(true);
  }

  function openEdit(task: Task) {
    previousFocusRef.current = document.activeElement as HTMLElement;
    setEditing(task);
    setShowModal(true);
  }

  function closeModal() {
    setShowModal(false);
    setEditing(null);
    previousFocusRef.current?.focus();
  }

  // モーダルが開いたら、モーダル自体(tabIndex=-1)へフォーカスを移す
  // (中の最初の入力欄まで踏み込むと、TaskForm側の実装に依存してしまうため、
  // 「ダイアログの入り口」に留める。スクリーンリーダーは role="dialog" と
  // aria-labelledby でタイトルを読み上げる)
  useEffect(() => {
    if (showModal) modalRef.current?.focus();
  }, [showModal]);

  // Escapeキーで閉じる、Tabキーでモーダルの外(背後の一覧)へフォーカスが
  // 漏れないようにする簡易フォーカストラップ
  function handleModalKeyDown(event: React.KeyboardEvent<HTMLDivElement>) {
    if (event.key === "Escape") {
      closeModal();
      return;
    }
    if (event.key !== "Tab" || !modalRef.current) return;
    const focusable = modalRef.current.querySelectorAll<HTMLElement>(
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
      <h1>タスク一覧(モーダル版UX)</h1>

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
        // Bootstrap 4.3.1 の modal マークアップを流用しつつ、開閉自体は React の state で管理する
        // jQuery 側の Bootstrap modal プラグインには依存しないテスト容易性と、フォームの値(React state)との同期を素直に保つため
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
