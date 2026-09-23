'use strict';

const jwtMod = require('./jwt');
const db = require('../db');
const { logDebug } = require('../logging');

// issクレームから認証モード(local_hmac/local_rsa/keycloak)のラベルを組み立てる。
// DEBUGログでどの経路(ローカルHMAC/ローカルRSA/Keycloak)で認証されたかを見分けるためだけに使う
function authModeLabel(iss) {
  if (iss === jwtMod.LOCAL_HMAC_ISSUER) return 'local_hmac';
  if (iss === jwtMod.LOCAL_RSA_ISSUER) return 'local_rsa';
  return 'keycloak';
}

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
    if (!user) return { ok: false };
    logDebug('resolved user', { user_id: user.id, auth_mode: authModeLabel(claims.iss), issuer: claims.iss });
    return { ok: true, userId: user.id };
  }

  const user = await db.findUserByKeycloakSub(pool, claims.sub);
  if (!user) return { ok: false };
  logDebug('resolved user', { user_id: user.id, auth_mode: 'keycloak', keycloak_sub: claims.sub });
  return { ok: true, userId: user.id };
}

module.exports = { resolveUserId };
