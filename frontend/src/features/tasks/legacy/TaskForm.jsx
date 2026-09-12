import { useEffect, useState } from "react";
import LabelSelect from "../LabelSelect";

const STATUS_OPTIONS = [
  { value: "waiting", label: "未着手" },
  { value: "work_in_progress", label: "進行中" },
  { value: "completed", label: "完了" },
];

// タスクの登録/更新で共用するフォーム(旧実装、JavaScript版)
//
// 【CONTRACT.mdセクション19.7】以前はlegacy/TaskList.jsx内にフォームJSXが直接埋め込まれて
// いたが、モーダル版(TaskListModal.jsx)・別ページ版(TaskFormPage.jsx)からも同じフォームを
// 再利用する必要が出たため、TaskForm.tsx(新実装)と対称的な形でこのファイルへ切り出した
// props/挙動はTaskForm.tsxと同一(型注釈が無いだけ)
export default function TaskForm({ initial, labelOptions, submitLabel, onSubmit }) {
  const [name, setName] = useState(initial?.name ?? "");
  const [description, setDescription] = useState(initial?.description ?? "");
  const [status, setStatus] = useState(initial?.status ?? "waiting");
  const [finishedOn, setFinishedOn] = useState(initial?.finishedOn ?? "");
  const [labelIds, setLabelIds] = useState((initial?.labels ?? []).map((l) => l.id));
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState(null);
  const [successMessage, setSuccessMessage] = useState(null);

  useEffect(() => {
    if (!successMessage) return undefined;
    const timer = setTimeout(() => setSuccessMessage(null), 3000);
    return () => clearTimeout(timer);
  }, [successMessage]);

  async function handleSubmit(event) {
    event.preventDefault();
    setSubmitting(true);
    setError(null);
    setSuccessMessage(null);
    try {
      await onSubmit({
        name,
        description: description === "" ? null : description,
        status,
        finishedOn,
        labelIds,
      });
      setSuccessMessage(initial ? "タスクを更新しました" : "タスクを作成しました");
      if (!initial) {
        setName("");
        setDescription("");
        setStatus("waiting");
        setFinishedOn("");
        setLabelIds([]);
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : "保存に失敗しました");
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <form onSubmit={handleSubmit} className="mb-4 border rounded p-3">
      {error && <div className="alert alert-danger" role="alert">{error}</div>}
      {successMessage && (
        <div className="alert alert-success" role="alert" data-testid="success-message">
          {successMessage}
        </div>
      )}
      <div className="mb-3">
        <label className="form-label" htmlFor="task-name">タスク名(20文字以内)</label>
        <input
          id="task-name"
          className="form-control"
          value={name}
          maxLength={20}
          required
          onChange={(e) => setName(e.target.value)}
          data-testid="task-name-input"
        />
      </div>
      <div className="mb-3">
        <label className="form-label" htmlFor="task-description">説明</label>
        <textarea
          id="task-description"
          className="form-control"
          value={description ?? ""}
          onChange={(e) => setDescription(e.target.value)}
        />
      </div>
      <div className="mb-3">
        <label className="form-label" htmlFor="task-status">ステータス</label>
        <select
          id="task-status"
          className="form-select"
          value={status}
          onChange={(e) => setStatus(e.target.value)}
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
          value={finishedOn}
          required
          onChange={(e) => setFinishedOn(e.target.value)}
          data-testid="task-finished-on-input"
        />
      </div>
      <div className="mb-3">
        <label className="form-label" htmlFor="task-labels">ラベル(複数選択可)</label>
        <LabelSelect options={labelOptions} value={labelIds} onChange={setLabelIds} />
      </div>
      <button
        type="submit"
        className="btn btn-primary"
        disabled={submitting}
        data-testid="task-submit-button"
      >
        {submitLabel}
      </button>
    </form>
  );
}
