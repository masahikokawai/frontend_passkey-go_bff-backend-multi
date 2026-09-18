'use strict';

const errors = require('../error');
const { resolveUserId } = require('../auth');

// backend(Go)のresolveUserID(REST v1版)と同じ: Authorizationヘッダを検証し、
// 内部user_idを解決する。JWT無し/検証失敗は401 unauthorized、
// 検証は通るがusersに該当行が無い場合は403 user_not_provisioned
async function authenticate(state, req) {
  const header = req.get('authorization');
  if (!header) throw errors.unauthorized();
  const m = /^Bearer (.+)$/.exec(header);
  if (!m) throw errors.unauthorized();

  let claims;
  try {
    claims = await state.dispatcher.verify(m[1]);
  } catch (e) {
    throw errors.unauthorized();
  }

  const resolved = await resolveUserId(state.pool, claims);
  if (!resolved.ok) throw errors.userNotProvisioned();
  return resolved.userId;
}

module.exports = { authenticate };
