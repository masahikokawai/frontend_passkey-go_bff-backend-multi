'use strict';

const test = require('node:test');
const assert = require('node:assert/strict');
const { taskToJson, parseLabelIds, toTaskInput } = require('../src/rest/task');
const { TaskStatus } = require('../src/model');

test('parseLabelIds splits on comma and ignores garbage', () => {
  assert.deepEqual(parseLabelIds(''), []);
  assert.deepEqual(parseLabelIds('1,2,3'), [1, 2, 3]);
  assert.deepEqual(parseLabelIds(' 1 , 2 '), [1, 2]);
  assert.deepEqual(parseLabelIds('1,x,3'), [1, 3]);
});

// CONTRACT.mdセクション5.1のJSON形状(スネークケース、user_idは含めない)と完全に一致することを確認する
test('taskToJson matches contract shape', () => {
  const task = {
    id: 1,
    name: 'buy milk',
    description: null,
    status: TaskStatus.WAITING,
    finishedOn: '2026-09-06',
    userId: 999,
    createdAt: '2026-09-06 12:00:00',
    updatedAt: '2026-09-06 12:00:00',
    labels: [{ id: 5, name: 'urgent' }],
  };

  const got = taskToJson(task);
  assert.equal(got.id, 1);
  assert.equal(got.name, 'buy milk');
  assert.equal(got.description, null);
  assert.equal(got.status, 'waiting');
  assert.equal(got.finished_on, '2026-09-06');
  assert.deepEqual(got.labels, [{ id: 5, name: 'urgent' }]);
  assert.equal(got.user_id, undefined);
  assert.equal(got.created_at, '2026-09-06T12:00:00+00:00');
});

test('toTaskInput rejects empty required fields', () => {
  assert.throws(() => toTaskInput({ name: '' }));
  assert.throws(() => toTaskInput({ name: 'x', status: '', finished_on: '2026-09-06' }));
  assert.throws(() => toTaskInput({ name: 'x', status: 'waiting', finished_on: '' }));
});

test('toTaskInput rejects unparseable finished_on', () => {
  assert.throws(() => toTaskInput({ name: 'x', status: 'waiting', finished_on: 'not-a-date' }));
});

test('toTaskInput rejects calendar-invalid finished_on', () => {
  assert.throws(() => toTaskInput({ name: 'x', status: 'waiting', finished_on: '2026-02-30' }));
});

test('toTaskInput accepts valid body', () => {
  const input = toTaskInput({
    name: 'x',
    status: 'waiting',
    finished_on: '2026-09-06',
    description: 'd',
    label_ids: [1, 2],
  });
  assert.equal(input.name, 'x');
  assert.deepEqual(input.labelIds, [1, 2]);
  assert.equal(input.finishedOn, '2026-09-06');
});
