// backend-js/src/auth/index.jsの型付き移植。ロジックは変更していない。

import type { Pool } from 'mysql2/promise';
import { isLocalIssuer, LOCAL_HMAC_ISSUER, LOCAL_RSA_ISSUER } from './jwt';
import * as db from '../db';
import type { JwtClaims } from '../types';
import { logDebug } from '../logging';

export type ResolveUserIdResult = { ok: true; userId: number } | { ok: false };

// issクレームから認証モード(local_hmac/local_rsa/keycloak)のラベルを組み立てる。
// DEBUGログでどの経路(ローカルHMAC/ローカルRSA/Keycloak)で認証されたかを見分けるためだけに使う
function authModeLabel(iss: string): string {
  if (iss === LOCAL_HMAC_ISSUER) return 'local_hmac';
  if (iss === LOCAL_RSA_ISSUER) return 'local_rsa';
  return 'keycloak';
}

// backend(Go)のresolveUserID(v1/task.go・grpcserver/task_service.go)と同じ分岐:
//   - ローカル(HMAC/RSA)発行のJWT: subは内部user_idそのもの -> usersをidで検索
//   - Keycloak発行のJWT: subはkeycloak_sub -> users.keycloak_subで検索
export async function resolveUserId(pool: Pool, claims: JwtClaims): Promise<ResolveUserIdResult> {
  if (isLocalIssuer(claims.iss)) {
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
