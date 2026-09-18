// backend-js/src/rest/authenticate.jsの型付き移植。ロジックは変更していない。

import type { Request } from 'express';
import * as errors from '../error';
import { resolveUserId } from '../auth';
import type { AppState } from '../types';

export async function authenticate(state: AppState, req: Request): Promise<number> {
  const header = req.get('authorization');
  if (!header) throw errors.unauthorized();
  const m = /^Bearer (.+)$/.exec(header);
  if (!m) throw errors.unauthorized();

  let claims;
  try {
    claims = await state.dispatcher.verify(m[1]!);
  } catch {
    throw errors.unauthorized();
  }

  const resolved = await resolveUserId(state.pool, claims);
  if (!resolved.ok) throw errors.userNotProvisioned();
  return resolved.userId;
}
