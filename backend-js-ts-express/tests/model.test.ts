// backend-js/tests/model.test.jsの型付き移植。テスト内容は変更していない。

import test from 'node:test';
import assert from 'node:assert/strict';
import {
  TaskStatus,
  statusToString,
  statusFromString,
  statusFromDb,
  validateTaskInput,
  codepointLength,
} from '../src/model';
import type { TaskInput } from '../src/types';

function validInput(today: string): TaskInput {
  return { name: 'buy milk', description: null, status: 'waiting', finishedOn: today, labelIds: [] };
}

test('validateTaskInput accepts valid input', () => {
  const today = '2026-09-09';
  const got = validateTaskInput(validInput(today), today);
  assert.equal(got.status, TaskStatus.WAITING);
});

test('validateTaskInput rejects empty name', () => {
  const today = '2026-09-09';
  const input = { ...validInput(today), name: '' };
  const got = validateTaskInput(input, today);
  assert.match(got.error!, /nameは必須です/);
});

test('validateTaskInput rejects name over 20 chars', () => {
  const today = '2026-09-09';
  const input = { ...validInput(today), name: 'a'.repeat(21) };
  const got = validateTaskInput(input, today);
  assert.match(got.error!, /20文字以内/);
});

test('validateTaskInput accepts name exactly 20 chars', () => {
  const today = '2026-09-09';
  const input = { ...validInput(today), name: 'a'.repeat(20) };
  assert.equal(validateTaskInput(input, today).error, undefined);
});

// 【重要な回帰テスト】コードポイント数で数える(UTF-16コード単位数ではない)
test('validateTaskInput accepts astral emoji name of exactly 20 codepoints', () => {
  const today = '2026-09-09';
  const name = '\u{1F600}'.repeat(20);
  assert.equal(codepointLength(name), 20);
  const input = { ...validInput(today), name };
  assert.equal(validateTaskInput(input, today).error, undefined);
});

test('validateTaskInput rejects astral emoji name of 21 codepoints', () => {
  const today = '2026-09-09';
  const name = '\u{1F600}'.repeat(21);
  const input = { ...validInput(today), name };
  const got = validateTaskInput(input, today);
  assert.match(got.error!, /20文字以内/);
});

test('validateTaskInput rejects past finished_on (UTC basis)', () => {
  const today = '2026-09-09';
  const input = { ...validInput(today), finishedOn: '2026-09-08' };
  const got = validateTaskInput(input, today);
  assert.match(got.error!, /過去日/);
});

test('validateTaskInput accepts finished_on equal to today', () => {
  const today = '2026-09-09';
  const input = validInput(today);
  assert.equal(validateTaskInput(input, today).error, undefined);
});

test('validateTaskInput rejects unknown status', () => {
  const today = '2026-09-09';
  const input = { ...validInput(today), status: 'not_a_status' };
  const got = validateTaskInput(input, today);
  assert.match(got.error!, /不明なstatus/);
});

test('task status round-trips through string and db value', () => {
  const cases: Array<[number, string, number]> = [
    [TaskStatus.WAITING, 'waiting', 1],
    [TaskStatus.WORK_IN_PROGRESS, 'work_in_progress', 2],
    [TaskStatus.COMPLETED, 'completed', 3],
  ];
  for (const [status, s, dbValue] of cases) {
    assert.equal(statusToString(status), s);
    assert.equal(statusFromString(s), status);
    assert.equal(statusFromDb(dbValue), status);
  }
  assert.equal(statusFromString('bogus'), null);
  assert.equal(statusFromDb(0), null);
  assert.equal(statusFromDb(99), null);
});
