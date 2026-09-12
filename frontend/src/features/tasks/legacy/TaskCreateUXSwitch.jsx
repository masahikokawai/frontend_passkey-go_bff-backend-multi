import { useStringFeatureFlag } from "@/shared/hooks/useFeatureFlag";
import TaskList from "./TaskList";
import TaskListModal from "./TaskListModal";
import TaskListPage from "./TaskListPage";

// Task登録UXの3パターン切り替え(旧実装/JavaScript版、CONTRACT.mdセクション19.7)
//
// TaskCreateUXSwitch.tsx(新実装)と全く同じロジック
// TaskListSwitch.tsx(frontend.tasks-ts-rewrite、TS/JS実装の切り替え)が false のときにこちらが使われる
export default function TaskCreateUXSwitch() {
  const ux = useStringFeatureFlag("frontend.task-create-ux", "inline");
  console.info(`[feature-flag] frontend.task-create-ux=${ux} を表示`);

  switch (ux) {
    case "modal":
      return <TaskListModal />;
    case "page":
      return <TaskListPage />;
    case "inline":
    default:
      return <TaskList />;
  }
}
