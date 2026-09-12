import { useEffect, useRef, useState } from "react";
import { createLabel, deleteLabel, fetchLabels, updateLabel } from "./api";

// ラベル一覧+CRUD(JavaScriptのまま
// CONTRACT.mdセクション6: labelsはTypeScript化の対象外
// ただし機能面はTask同様に「使える一連の操作」を揃える、CONTRACT.mdセクション14)
//
// Rails: Label#name は必須・一意・10文字以内(role_managementのみCRUD可)
// このアプリでは権限チェックはbackend/bffのFeature Flag学習が主眼のため簡略化し、
// ログイン済みユーザーなら誰でも編集できる(既存のTask操作と同じ扱い)
export default function LabelList() {
  const [labels, setLabels] = useState([]);
  const [loading, setLoading] = useState(true);
  const [name, setName] = useState("");
  const [editingId, setEditingId] = useState(null);
  const [editingName, setEditingName] = useState("");
  const [error, setError] = useState(null);
  const [message, setMessage] = useState(null);
  // 【テスト監査で発見・修正】create/update/deleteはいずれも成功後にreload()を呼ぶため、
  // ユーザーが立て続けに2つの操作を行うと2つのreload()が重なり得る
  // (例: 削除ボタンを押した直後、deleteLabel()自体が解決するまでは画面が操作可能なままで、
  // その間に登録フォームから別のラベルを作成する、等)。fetchLabelsの応答が
  // リクエストを送った順番通りに返ってくる保証は無いため、後に呼ばれたreload()の応答が
  // 先に、先に呼ばれたreload()の応答が後に届くと、新しい一覧が古い一覧で上書きされてしまう
  // (frontend/src/features/tasks/TaskList.tsx等と全く同じ理由・同じ対策)
  const latestRequestIdRef = useRef(0);
  // 作成・更新の送信中はボタンをdisabledにし、二重送信を防ぐ
  // (TaskForm.tsxのsubmittingガードと同じ考え方。以前はここに無く、
  // 送信中に連打すると2つのcreateLabel/updateLabelが同時に飛び得た)
  const [submitting, setSubmitting] = useState(false);

  async function reload() {
    const requestId = ++latestRequestIdRef.current;
    setLoading(true);
    // 以前はここにtry/catchが無く、一覧取得が失敗すると「読み込み中...」のまま固まり、
    // 未処理のPromise rejectionになりユーザーには何も表示されなかった
    try {
      const res = await fetchLabels();
      if (requestId !== latestRequestIdRef.current) return;
      setLabels(res.labels);
      setError(null);
    } catch (err) {
      if (requestId !== latestRequestIdRef.current) return;
      setError(err instanceof Error ? err.message : "一覧の取得に失敗しました");
    } finally {
      if (requestId === latestRequestIdRef.current) setLoading(false);
    }
  }

  useEffect(() => {
    reload();
  }, []);

  // 成功メッセージは数秒で自動的に消す(CONTRACT.mdセクション15と同じ考え方)
  useEffect(() => {
    if (!message) return undefined;
    const timer = setTimeout(() => setMessage(null), 3000);
    return () => clearTimeout(timer);
  }, [message]);

  async function handleCreate(event) {
    event.preventDefault();
    setError(null);
    setSubmitting(true);
    try {
      await createLabel(name);
      setName("");
      setMessage("ラベルを作成しました");
      await reload();
    } catch (err) {
      setError(err instanceof Error ? err.message : "作成に失敗しました");
    } finally {
      setSubmitting(false);
    }
  }

  function startEdit(label) {
    setEditingId(label.id);
    setEditingName(label.name);
    setError(null);
  }

  async function handleUpdate(event) {
    event.preventDefault();
    setError(null);
    setSubmitting(true);
    try {
      await updateLabel(editingId, editingName);
      setEditingId(null);
      setMessage("ラベルを更新しました");
      await reload();
    } catch (err) {
      setError(err instanceof Error ? err.message : "更新に失敗しました");
    } finally {
      setSubmitting(false);
    }
  }

  async function handleDelete(label) {
    if (!window.confirm("このラベルを削除しますか?")) return;
    setError(null);
    // 以前はここにtry/catchが無く、削除APIが失敗すると未処理のPromise rejectionに
    // なりユーザーには何も表示されなかった(create/updateのhandlerと同じ扱いにする)
    try {
      await deleteLabel(label.id);
      setMessage("ラベルを削除しました");
      await reload();
    } catch (err) {
      setError(err instanceof Error ? err.message : "削除に失敗しました");
    }
  }

  if (loading) {
    return <div>読み込み中...</div>;
  }

  return (
    <div>
      <h1>ラベル一覧</h1>
      {error && <div className="alert alert-danger" role="alert">{error}</div>}
      {message && (
        <div className="alert alert-success" role="alert" data-testid="success-message">
          {message}
        </div>
      )}

      <form onSubmit={handleCreate} className="mb-3 d-flex gap-2">
        {/* 【テスト監査で発見・修正】visually-hidden(視覚上は非表示、支援技術には見える)の
            labelが無く、placeholderだけが名前の手がかりだった。placeholderは入力中に消える上、
            スクリーンリーダーの読み上げ対象として保証されないため、htmlFor/idで正式に関連付ける */}
        <label className="sr-only" htmlFor="label-name">新しいラベル名(10文字以内)</label>
        <input
          id="label-name"
          className="form-control"
          value={name}
          required
          maxLength={10}
          placeholder="新しいラベル名(10文字以内)"
          onChange={(e) => setName(e.target.value)}
          data-testid="label-name-input"
        />
        <button
          type="submit"
          className="btn btn-primary"
          disabled={submitting}
          data-testid="label-submit-button"
        >
          作成
        </button>
      </form>

      <ul className="list-group">
        {labels.map((label) => (
          <li
            key={label.id}
            className="list-group-item d-flex justify-content-between align-items-center"
            data-testid="label-row"
            data-label-name={label.name}
          >
            {editingId === label.id ? (
              <form onSubmit={handleUpdate} className="d-flex gap-2 flex-grow-1">
                <label className="sr-only" htmlFor={`label-edit-${label.id}`}>
                  ラベル名(10文字以内)
                </label>
                <input
                  id={`label-edit-${label.id}`}
                  className="form-control"
                  value={editingName}
                  required
                  maxLength={10}
                  onChange={(e) => setEditingName(e.target.value)}
                  data-testid="label-edit-input"
                />
                <button
                  type="submit"
                  className="btn btn-sm btn-primary"
                  disabled={submitting}
                  data-testid="label-save-button"
                >
                  保存
                </button>
                <button
                  type="button"
                  className="btn btn-sm btn-outline-secondary"
                  disabled={submitting}
                  onClick={() => setEditingId(null)}
                >
                  キャンセル
                </button>
              </form>
            ) : (
              <>
                <span>{label.name}</span>
                <div>
                  <button
                    type="button"
                    className="btn btn-sm btn-outline-secondary me-2"
                    onClick={() => startEdit(label)}
                    data-testid="label-edit-button"
                  >
                    編集
                  </button>
                  <button
                    type="button"
                    className="btn btn-sm btn-outline-danger"
                    onClick={() => handleDelete(label)}
                    data-testid="label-delete-button"
                  >
                    削除
                  </button>
                </div>
              </>
            )}
          </li>
        ))}
      </ul>
    </div>
  );
}
