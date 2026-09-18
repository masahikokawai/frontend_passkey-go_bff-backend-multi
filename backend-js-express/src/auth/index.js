'use strict';

const jwtMod = require('./jwt');
const db = require('../db');

// backend(Go)のresolveUserID(v1/task.go・grpcserver/task_service.go)と同じ分岐:
//   - ローカル(HMAC/RSA)発行のJWT: subは内部user_idそのもの -> usersをidで検索
//   - Keycloak発行のJWT: subはkeycloak_sub -> users.keycloak_subで検索
// どちらも見つからなければ{ok:false}(REST側403 user_not_provisioned /
// gRPC側codes.PermissionDenied "user not provisioned"に対応する)
async function resolveUserId(pool, claims) {
  if (jwtMod.isLocalIssuer(claims.iss)) {
    const id = Number(claims.sub);
    if (!Number.isInteger(id) || id <= 0) return { ok: false };
    const user = await db.findUserById(pool, id);
    return user ? { ok: true, userId: user.id } : { ok: false };
  }

  const user = await db.findUserByKeycloakSub(pool, claims.sub);
  return user ? { ok: true, userId: user.id } : { ok: false };
}

module.exports = { resolveUserId };
