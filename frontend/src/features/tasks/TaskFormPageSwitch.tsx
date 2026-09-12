import { useFeatureFlag } from "@/shared/hooks/useFeatureFlag";
import TaskFormPage from "./TaskFormPage";
import LegacyTaskFormPage from "./legacy/TaskFormPage";

interface TaskFormPageSwitchProps {
  mode: "create" | "edit";
}

// /tasks/new・/tasks/:id/edit ルート用のTS/JS切り替え(CONTRACT.mdセクション19.7)
//
// TaskListSwitch.tsxと同じ frontend.tasks-ts-rewrite フラグで、
// 専用ページの TypeScript 版/JavaScript版を切り替える
// TaskListPage(一覧)側から「タスクを登録」
// 「編集」リンクで遷移してくるが、リンク自体は現在表示中の実装(TS/JS)がどちらでも
// 同じ /tasks/new, /tasks/:id/edit を指すため、遷移先であるこのルートの方で
// 「今どちらの実装を使うべきか」をこのフラグから解決する
export default function TaskFormPageSwitch({ mode }: TaskFormPageSwitchProps) {
  const useNewImplementation = useFeatureFlag("frontend.tasks-ts-rewrite", false);
  return useNewImplementation ? <TaskFormPage mode={mode} /> : <LegacyTaskFormPage mode={mode} />;
}
