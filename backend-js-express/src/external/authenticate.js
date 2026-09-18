'use strict';

const errors = require('../error');

// backend(Go)のauthjwt.RequireExternalClientAuthと同じ2段チェック:
// (1) KeycloakのJWKSで署名検証、(2) `azp`クレームが期待するクライアントIDと一致するか
async function authenticate(state, req) {
  const header = req.get('authorization');
  if (!header) throw errors.unauthorized();
  const m = /^Bearer (.+)$/.exec(header);
  if (!m || !m[1]) throw errors.unauthorized();

  let claims;
  try {
    claims = await state.keycloakVerifier.verify(m[1]);
  } catch (e) {
    throw errors.invalidToken();
  }

  if (claims.azp !== state.expectedClientId) {
    throw errors.clientNotAllowed();
  }
}

module.exports = { authenticate };
