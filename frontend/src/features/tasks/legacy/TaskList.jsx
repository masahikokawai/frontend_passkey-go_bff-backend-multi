import { useCallback, useEffect, useRef, useState } from "react";
import { apiFetch } from "@/shared/api/client";
import { createTask, deleteTask, fetchTasks, updateTask } from "../api";
import LabelSelect from "../LabelSelect";

// タスク一覧(旧実装、JavaScript)
//
// これは実際に稼働していた旧コードではなく、Feature Flagによる新旧切り替え
// (Strangler Fig / Release Toggle)を学習用に体験するため、あえて用意した重複実装
//
// 【重要】
// 以前はここを一覧表示のみに機能を絞っていたが、
// これは実務の Strangler Figの前提と矛盾する
// (旧実装はまだ現役の本番コードである以上、機能は新実装と同等に揃っていなければならないFlagを倒しても「一部の機能が急に使えなくなる」のは正しい移行とは言えない)
// そのため一覧/登録/更新/削除のフル機能を、TaskList.tsx(新実装)と同じAPI(../api.ts)を使って実装している
// 違いは「型が無い JavaScript で書かれていること」だけに留め、機能面の差はもう無い
//
// 【CONTRACT.mdセクション19.7】
// モーダル版(TaskListModal.jsx)・別ページ版(TaskFormPage.jsx)用に共用フォーム(legacy/TaskForm.jsx)を新設したが、
// この inline 版はあえて独自のフォーム JSX・message/error ステートのまま維持している
//
// TaskForm.jsxは送信成功時に自分自身の内部stateで
// 成功メッセージを表示する設計だが、このファイルのようにeditingIdクリアと同時に
// <TaskForm key=.../>を再マウントする構成だと、メッセージがマウント直後に消えてしまうタイミング問題がある
//
// フォームJSX自体は多少重複するが、この画面固有の見た目・タイミングを壊さないことを優先した
export default function TaskList() {
  const [tasks, setTasks] = useState([]);
  const [labelOptions, setLabelOptions] = useState([]);
  const [loading, setLoading] = useState(true);
  const [editingId, setEditingId] = useState(null);
  const [form, setForm] = useState(emptyForm());
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState(null);
  const [message, setMessage] = useState(null);

  // 成功メッセージは数秒で自動的に消す(CONTRACT.mdセクション15、TaskForm.tsxと同じ考え方)
  useEffect(() => {
    if (!message) return undefined;
    const timer = setTimeout(() => setMessage(null), 3000);
    return () => clearTimeout(timer);
  }, [message]);

  // 【テスト監査で発見・修正】TaskList.tsx(新実装)と同じstale-response上書きバグが
  // ここにもあった。reload()は削除・作成・更新の各成功時にも呼ばれるため、ユーザーが
  // 立て続けに2つの操作を行うと2つのreload()が同時に実行され得る(例: 行Aの削除→即座に
  // 行Bの編集を送信)。fetchTasksの応答がリクエストを送った順番通りに返ってくる保証は
  // 無いため、後に呼ばれたreload()の応答が先に、先に呼ばれたreload()の応答が後に届くと、
  // 新しい一覧が古い一覧で上書きされてしまう(表示上は何もエラーが出ないまま、直前の
  // 操作結果が消える)。reload()呼び出しごとに採番し、応答が返ってきた時点で
  // 「自分が最新のreload()か」を確認することで、古い応答を無視する
  const latestRequestIdRef = useRef(0);
  const reload = useCallback(async () => {
    const requestId = ++latestRequestIdRef.current;
    setLoading(true);
    // 以前はここにtry/catchが無く、一覧取得が失敗すると「読み込み中...」のまま固まり、
    // 未処理のPromise rejectionになりユーザーには何も表示されなかった
    try {
      const res = await fetchTasks();
      if (requestId !== latestRequestIdRef.current) return;
      setTasks(res.tasks);
      setError(null);
    } catch (err) {
      if (requestId !== latestRequestIdRef.current) return;
      setError(err instanceof Error ? err.message : "一覧の取得に失敗しました");
    } finally {
      if (requestId === latestRequestIdRef.current) setLoading(false);
    }
  }, []);

  useEffect(() => {
    reload();
    apiFetch("/api/labels").then((res) => setLabelOptions(res.labels));
  }, [reload]);

  function startEdit(task) {
    setEditingId(task.id);
    setForm({
      name: task.name,
      description: task.description ?? "",
      status: task.status,
      finishedOn: task.finishedOn,
      labelIds: (task.labels ?? []).map((l) => l.id),
    });
  }

  async function handleSubmit(event) {
    event.preventDefault();
    setSubmitting(true);
    setError(null);
    setMessage(null);
    const input = {
      name: form.name,
      description: form.description === "" ? null : form.description,
      status: form.status,
      finishedOn: form.finishedOn,
      labelIds: form.labelIds,
    };
    try {
      if (editingId) {
        await updateTask(editingId, input);
        setEditingId(null);
        setMessage("タスクを更新しました");
      } else {
        await createTask(input);
        setMessage("タスクを作成しました");
      }
      setForm(emptyForm());
      await reload();
    } catch (err) {
      setError(err instanceof Error ? err.message : "保存に失敗しました");
    } finally {
      setSubmitting(false);
    }
  }

  async function handleDelete(task) {
    if (!window.confirm("このタスクを削除しますか?")) return;
    setError(null);
    // 以前はここにtry/catchが無く、削除APIが失敗すると未処理のPromise rejectionに
    // なりユーザーには何も表示されなかった(create/updateのhandleSubmitと同じ扱いにする)
    try {
      await deleteTask(task.id);
      if (editingId === task.id) {
        setEditingId(null);
        setForm(emptyForm());
      }
      setMessage("タスクを削除しました");
      await reload();
    } catch (err) {
      setError(err instanceof Error ? err.message : "削除に失敗しました");
    }
  }

  if (loading) {
    return <div>読み込み中...</div>;
  }

  return (
    <div data-testid="task-list">
      <h1>タスク一覧(旧実装 / JavaScript)</h1>

      <form onSubmit={handleSubmit} className="mb-4 border rounded p-3">
        {error && <div className="alert alert-danger" role="alert">{error}</div>}
        {message && (
          <div className="alert alert-success" role="alert" data-testid="success-message">
            {message}
          </div>
        )}
        <div className="mb-3">
          <label className="form-label" htmlFor="task-name">タスク名(20文字以内)</label>
          <input
            id="task-name"
            className="form-control"
            value={form.name}
            maxLength={20}
            required
            onChange={(e) => setForm({ ...form, name: e.target.value })}
            data-testid="task-name-input"
          />
        </div>
        <div className="mb-3">
          <label className="form-label" htmlFor="task-description">説明</label>
          <textarea
            id="task-description"
            className="form-control"
            value={form.description}
            onChange={(e) => setForm({ ...form, description: e.target.value })}
          />
        </div>
        <div className="mb-3">
          <label className="form-label" htmlFor="task-status">ステータス</label>
          <select
            id="task-status"
            className="form-select"
            value={form.status}
            onChange={(e) => setForm({ ...form, status: e.target.value })}
            data-testid="task-status-select"
          >
            {STATUS_OPTIONS.map((opt) => (
              <option key={opt.value} value={opt.value}>
                {opt.label}
              </option>
            ))}
          </select>
        </div>
        <div className="mb-3">
          <label className="form-label" htmlFor="task-finished-on">期限(過去日不可)</label>
          <input
            id="task-finished-on"
            type="date"
            className="form-control"
            value={form.finishedOn}
            required
            onChange={(e) => setForm({ ...form, finishedOn: e.target.value })}
            data-testid="task-finished-on-input"
          />
        </div>
        <div className="mb-3">
          <label className="form-label" htmlFor="task-labels">ラベル(複数選択可)</label>
          <LabelSelect
            options={labelOptions}
            value={form.labelIds}
            onChange={(labelIds) => setForm({ ...form, labelIds })}
          />
        </div>
        <button type="submit" className="btn btn-primary" disabled={submitting} data-testid="task-submit-button">
          {editingId ? "更新" : "作成"}
        </button>
      </form>

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
              <td>{(task.labels ?? []).map((label) => label.name).join(", ")}</td>
              <td>
                <button
                  type="button"
                  className="btn btn-sm btn-outline-secondary me-2"
                  onClick={() => startEdit(task)}
                  data-testid="task-edit-button"
                >
                  編集
                </button>
                <button
                  type="button"
                  className="btn btn-sm btn-outline-danger"
                  onClick={() => handleDelete(task)}
                  data-testid="task-delete-button"
                >
                  削除
                </button>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

const STATUS_OPTIONS = [
  { value: "waiting", label: "未着手" },
  { value: "work_in_progress", label: "進行中" },
  { value: "completed", label: "完了" },
];

function emptyForm() {
  return { name: "", description: "", status: "waiting", finishedOn: "", labelIds: [] };
}
