'use strict';

const test = require('node:test');
const assert = require('node:assert/strict');
const errors = require('../src/error');

// backend(Go)・backend-rustのエラー分岐と1対1で対応することを確認する
// (CONTRACT.mdセクション20.5のワイヤー契約パリティ)
test('error factories produce the exact contract status/body shape', () => {
  const cases = [
    [errors.unauthorized(), 401, { error: 'unauthorized' }],
    [errors.userNotProvisioned(), 403, { error: 'user_not_provisioned' }],
    [errors.invalidRequest(), 400, { error: 'invalid_request' }],
    [errors.invalidId(), 400, { error: 'invalid_id' }],
    [errors.invalidStatus(), 422, { error: 'invalid_status' }],
    [errors.invalidFinishedOn(), 422, { error: 'invalid_finished_on' }],
    [errors.validationError('nameは必須です'), 422, { error: 'validation_error', message: 'nameは必須です' }],
    [errors.notFound(), 404, { error: 'not_found' }],
    [errors.internal(), 500, { error: 'internal_server_error' }],
    [errors.invalidToken(), 401, { error: 'invalid_token' }],
    [errors.clientNotAllowed(), 403, { error: 'client_not_allowed' }],
    [errors.userIdRequired(), 400, { error: 'user_id is required' }],
    [errors.invalidUserId(), 400, { error: 'invalid user_id' }],
  ];
  for (const [err, status, body] of cases) {
    assert.equal(err.status, status, `status mismatch for ${err.kind}`);
    assert.deepEqual(err.body, body, `body mismatch for ${err.kind}`);
  }
});
