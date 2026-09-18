// backend-js/src/model.jsの型付き移植。ロジックは変更していない。

import type { TaskInput } from './types';

export const TaskStatus = Object.freeze({
  WAITING: 1,
  WORK_IN_PROGRESS: 2,
  COMPLETED: 3,
});

export type TaskStatusValue = (typeof TaskStatus)[keyof typeof TaskStatus];

const STATUS_TO_STRING: Readonly<Record<number, string>> = Object.freeze({
  1: 'waiting',
  2: 'work_in_progress',
  3: 'completed',
});

const STRING_TO_STATUS: Readonly<Record<string, number>> = Object.freeze({
  waiting: TaskStatus.WAITING,
  work_in_progress: TaskStatus.WORK_IN_PROGRESS,
  completed: TaskStatus.COMPLETED,
});

export function statusToString(status: number): string | null {
  return STATUS_TO_STRING[status] ?? null;
}

export function statusFromString(s: string): number | null {
  return STRING_TO_STATUS[s] ?? null;
}

export function statusFromDb(v: number): number | null {
  return STATUS_TO_STRING[v] !== undefined ? v : null;
}

// コードポイント数で数える(UTF-16コード単位数ではない)。
// [...str]はサロゲートペア(絵文字等)を1コードポイントとして数えるため、
// backend(Go)のlen([]rune(name))・backend-rustの.chars().count()と同じ挙動になる。
// 【過去に発見されたバグの回帰防止】backend-scala-http4sはUTF-16コード単位数で数えており、
// 基本多言語面外の文字(絵文字等)を含む名前で20文字境界の判定がズレていた
// (CONTRACT.mdセクション23.1、TaskService.scalaのコメント参照)
export function codepointLength(s: string): number {
  return [...s].length;
}

export type ValidateResult = { status: number; error?: undefined } | { status?: undefined; error: string };

// backend(Go)のservice.validateTaskInputと同じルール:
//   - name: 必須・20文字(コードポイント)以内
//   - finished_on: 「今日」より過去は不可。todayIsoは呼び出し側がUTC基準で計算して渡す
//   - status: enumの範囲内
export function validateTaskInput(input: TaskInput, todayIso: string): ValidateResult {
  if (!input.name) {
    return { error: 'nameは必須です' };
  }
  if (codepointLength(input.name) > 20) {
    return { error: 'nameは20文字以内である必要があります' };
  }
  if (input.finishedOn < todayIso) {
    return { error: 'finished_onに過去日は指定できません' };
  }
  const status = statusFromString(input.status);
  if (status === null) {
    return { error: `不明なstatus: "${input.status}"` };
  }
  return { status };
}

export function todayUtcIso(): string {
  return new Date().toISOString().slice(0, 10);
}
