// backend-js/src/external/authenticate.jsの型付き移植。ロジックは変更していない。

import type { Request } from 'express';
import * as errors from '../error';
import type { ExternalState } from '../types';

// backend(Go)のauthjwt.RequireExternalClientAuthと同じ2段チェック:
// (1) KeycloakのJWKSで署名検証、(2) `azp`クレームが期待するクライアントIDと一致するか
export async function authenticate(state: ExternalState, req: Request): Promise<void> {
  const header = req.get('authorization');
  if (!header) throw errors.unauthorized();
  const m = /^Bearer (.+)$/.exec(header);
  if (!m || !m[1]) throw errors.unauthorized();

  let claims;
  try {
    claims = await state.keycloakVerifier.verify(m[1]);
  } catch {
    throw errors.invalidToken();
  }

  if (claims.azp !== state.expectedClientId) {
    throw errors.clientNotAllowed();
  }
}
