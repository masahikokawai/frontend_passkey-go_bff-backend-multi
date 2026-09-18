// backend-js/src/auth/index.jsの型付き移植。ロジックは変更していない。

import type { Pool } from 'mysql2/promise';
import { isLocalIssuer } from './jwt';
import * as db from '../db';
import type { JwtClaims } from '../types';

export type ResolveUserIdResult = { ok: true; userId: number } | { ok: false };

// backend(Go)のresolveUserID(v1/task.go・grpcserver/task_service.go)と同じ分岐:
//   - ローカル(HMAC/RSA)発行のJWT: subは内部user_idそのもの -> usersをidで検索
//   - Keycloak発行のJWT: subはkeycloak_sub -> users.keycloak_subで検索
export async function resolveUserId(pool: Pool, claims: JwtClaims): Promise<ResolveUserIdResult> {
  if (isLocalIssuer(claims.iss)) {
    const id = Number(claims.sub);
    if (!Number.isInteger(id) || id <= 0) return { ok: false };
    const user = await db.findUserById(pool, id);
    return user ? { ok: true, userId: user.id } : { ok: false };
  }

  const user = await db.findUserByKeycloakSub(pool, claims.sub);
  return user ? { ok: true, userId: user.id } : { ok: false };
}
