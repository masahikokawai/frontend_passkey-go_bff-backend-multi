import { useFeatureFlag } from "@/shared/hooks/useFeatureFlag";
import LegacyTaskCreateUXSwitch from "./legacy/TaskCreateUXSwitch";
import TaskCreateUXSwitch from "./TaskCreateUXSwitch";

// Feature Flagによる新旧実装切り替え(Strangler Fig / Release Toggle)のデモ
// CONTRACT.md セクション4: "frontend.tasks-ts-rewrite" が対象
//
// 運用上の注意(このデモの本質): これは恒久的に残すフラグではない
// 新実装(TaskList.tsx)が安定したと判断した時点で、
//   - このコンポーネント自体
//   - ./legacy/ 配下一式
//   - flags.yaml上の "frontend.tasks-ts-rewrite" 定義
// をすべて削除し、ルーティングは TaskList.tsx を直接指すようにすること
//
// 【CONTRACT.mdセクション19.7】frontend.task-create-ux(登録UXの3パターン)は
// 当初TypeScript実装側にのみ適用していたが、「TS/JS」×「inline/modal/page」の
// 2軸を完全に独立・組み合わせ可能にしてほしいというユーザーからの要望により、
// 旧実装(JavaScript)側にも同じ3パターンを実装した(legacy/TaskCreateUXSwitch.jsx)
// 2つのフラグはそれぞれ独立して評価され、結果として2×3=6通りの組み合わせが
// すべて到達可能になっている
export default function TaskListSwitch() {
  const useNewImplementation = useFeatureFlag("frontend.tasks-ts-rewrite", false);
  // 切り替えが実際にどちらへ効いたかブラウザのコンソールから追えるようにする
  console.info(
    `[feature-flag] frontend.tasks-ts-rewrite=${useNewImplementation} → ${
      useNewImplementation ? "新実装(TypeScript, TaskList.tsx)" : "旧実装(JavaScript, legacy/TaskList.jsx)"
    }を表示`
  );
  return useNewImplementation ? <TaskCreateUXSwitch /> : <LegacyTaskCreateUXSwitch />;
}
