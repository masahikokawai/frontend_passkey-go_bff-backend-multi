interface TaskDeleteButtonProps {
  onDelete: () => Promise<void>;
}

// Rails: `link_to '削除', task, method: :delete, data: { confirm: '...' }` に相当
export default function TaskDeleteButton({ onDelete }: TaskDeleteButtonProps) {
  async function handleClick() {
    if (!window.confirm("このタスクを削除しますか?")) return;
    await onDelete();
  }

  return (
    <button
      type="button"
      className="btn btn-sm btn-outline-danger"
      onClick={handleClick}
      data-testid="task-delete-button"
    >
      削除
    </button>
  );
}
