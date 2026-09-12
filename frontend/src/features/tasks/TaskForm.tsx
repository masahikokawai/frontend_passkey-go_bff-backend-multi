import { useEffect, useState, type FormEvent } from "react";
import LabelSelect from "./LabelSelect";
import type { Label, Task, TaskInput, TaskStatus } from "./types";

interface TaskFormProps {
  initial?: Task;
  labelOptions: Label[];
  submitLabel: string;
  onSubmit: (input: TaskInput) => Promise<void>;
}

// Rails: Task#status enum(waiting/work_in_progress/completed)に対応
const STATUS_OPTIONS: { value: TaskStatus; label: string }[] = [
  { value: "waiting", label: "未着手" },
  { value: "work_in_progress", label: "進行中" },
  { value: "completed", label: "完了" },
];

// タスクの登録/更新で共用するフォーム
// Rails: app/views/tasks/_form.html.erb に相当
export default function TaskForm({ initial, labelOptions, submitLabel, onSubmit }: TaskFormProps) {
  const [name, setName] = useState(initial?.name ?? "");
  const [description, setDescription] = useState(initial?.description ?? "");
  const [status, setStatus] = useState<TaskStatus>(initial?.status ?? "waiting");
  const [finishedOn, setFinishedOn] = useState(initial?.finishedOn ?? "");
  const [labelIds, setLabelIds] = useState<number[]>((initial?.labels ?? []).map((l) => l.id));
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [successMessage, setSuccessMessage] = useState<string | null>(null);

  // 成功メッセージは数秒で自動的に消す(CONTRACT.mdセクション15)
  useEffect(() => {
    if (!successMessage) return undefined;
    const timer = setTimeout(() => setSuccessMessage(null), 3000);
    return () => clearTimeout(timer);
  }, [successMessage]);

  async function handleSubmit(event: FormEvent) {
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
          onChange={(e) => setStatus(e.target.value as TaskStatus)}
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
