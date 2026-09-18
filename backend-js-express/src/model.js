'use strict';

// backend(Go)の internal/model/enum.go の TaskStatus (waiting=1, work_in_progress=2, completed=3) と
// 完全に同じマッピングを再現する
const TaskStatus = Object.freeze({
  WAITING: 1,
  WORK_IN_PROGRESS: 2,
  COMPLETED: 3,
});

const STATUS_TO_STRING = Object.freeze({
  1: 'waiting',
  2: 'work_in_progress',
  3: 'completed',
});

const STRING_TO_STATUS = Object.freeze({
  waiting: TaskStatus.WAITING,
  work_in_progress: TaskStatus.WORK_IN_PROGRESS,
  completed: TaskStatus.COMPLETED,
});

function statusToString(status) {
  return STATUS_TO_STRING[status] ?? null;
}

function statusFromString(s) {
  return STRING_TO_STATUS[s] ?? null;
}

function statusFromDb(v) {
  return STATUS_TO_STRING[v] !== undefined ? v : null;
}

// コードポイント数で数える(UTF-16コード単位数ではない)。
// [...str]はサロゲートペア(絵文字等)を1コードポイントとして数えるため、
// backend(Go)のlen([]rune(name))・backend-rustの.chars().count()と同じ挙動になる。
// 【過去に発見されたバグの回帰防止】backend-scala-http4sはUTF-16コード単位数で数えており、
// 基本多言語面外の文字(絵文字等)を含む名前で20文字境界の判定がズレていた
// (CONTRACT.mdセクション23.1、TaskService.scalaのコメント参照)
function codepointLength(s) {
  return [...s].length;
}

// backend(Go)のservice.validateTaskInputと同じルール:
//   - name: 必須・20文字(コードポイント)以内
//   - finished_on: 「今日」より過去は不可。todayIsoは呼び出し側がUTC基準で計算して渡す
//     (過去に発見されたタイムゾーン不整合バグ、CONTRACT.mdセクション23.1の教訓を踏まえ、
//     この実装は最初からUTC基準以外の経路を持たない)
//   - status: enumの範囲内
// 戻り値: {status} または {error}
function validateTaskInput(input, todayIso) {
  if (!input.name) {
    return { error: 'nameは必須です' };
  }
  if (codepointLength(input.name) > 20) {
    return { error: 'nameは20文字以内である必要があります' };
  }
  // ISO 8601形式(YYYY-MM-DD)の日付文字列は辞書順比較がそのまま日付の前後関係と一致する
  if (input.finishedOn < todayIso) {
    return { error: 'finished_onに過去日は指定できません' };
  }
  const status = statusFromString(input.status);
  if (status === null) {
    return { error: `不明なstatus: "${input.status}"` };
  }
  return { status };
}

function todayUtcIso() {
  return new Date().toISOString().slice(0, 10);
}

module.exports = {
  TaskStatus,
  statusToString,
  statusFromString,
  statusFromDb,
  codepointLength,
  validateTaskInput,
  todayUtcIso,
};
