import { useOutletContext } from "react-router-dom";

// CONTRACT.mdセクション19: frontend.task-create-ux のような多値(文字列)フラグに対応するため、
// booleanだけでなくstringも許容する型に一般化した(既存のbooleanフラグの型は壊さない)
export interface FeatureFlagContext {
  featureFlags: Record<string, boolean | string>;
}

// Feature Flagの値を参照するフック
//
// 重要: このフックはBFFの /api/me が返した評価済みの値を読むだけであり、
// フロントエンドが自分でFlag providerに問い合わせることはしない
// providerの接続情報・APIキーをブラウザに露出させないためのBFF集約パターン
// (CONTRACT.md セクション4)を維持するための設計
export function useFeatureFlag(key: string, defaultValue: boolean): boolean {
  const context = useOutletContext<FeatureFlagContext | undefined>();
  const value = context?.featureFlags[key];
  return typeof value === "boolean" ? value : defaultValue;
}

// useFeatureFlagの文字列版(CONTRACT.mdセクション19、frontend.task-create-ux用)
// 値が無い(backend/bff側の反映がまだ済んでいない等)場合や型が一致しない場合は
// 安全にdefaultValueへフォールバックする
export function useStringFeatureFlag(key: string, defaultValue: string): string {
  const context = useOutletContext<FeatureFlagContext | undefined>();
  const value = context?.featureFlags[key];
  return typeof value === "string" ? value : defaultValue;
}
