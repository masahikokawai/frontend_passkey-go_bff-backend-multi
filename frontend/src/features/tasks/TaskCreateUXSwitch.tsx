import { useStringFeatureFlag } from "@/shared/hooks/useFeatureFlag";
import TaskList from "./TaskList";
import TaskListModal from "./TaskListModal";
import TaskListPage from "./TaskListPage";

// Task登録UXの3パターン切り替え(CONTRACT.mdセクション19、frontend.task-create-ux)
//
// TaskListSwitch(frontend.tasks-ts-rewrite、TS/JS実装の切り替え)とは直交する別軸:
// こちらは新実装(TypeScript)の中で「登録/編集フォームをどう見せるか」
// (一覧上部の常設フォーム/モーダル/別ページ遷移)を切り替える
export default function TaskCreateUXSwitch() {
  const ux = useStringFeatureFlag("frontend.task-create-ux", "inline");
  // 切り替えが実際にどのUXへ効いたかブラウザのコンソールから追えるようにする
  // (TaskListSwitch.tsxのconsole.infoパターンを踏襲)
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
