'use strict';

// backend(Go)の internal/authjwt パッケージ(dispatcher.go/jwks.go/hmac.go)の再現。
// Keycloak発行(JWKS/RS256)・ローカルHMAC発行(HS256)・ローカルRSA発行(JWKS/RS256、
// bffの/.well-known/jwks.jsonから取得)の3issuerを、tokenのiss(署名検証前に覗いた値)で
// 振り分ける2段構造(Dispatcher)も同じにしている。

const jwt = require('jsonwebtoken');
const crypto = require('node:crypto');

const LOCAL_HMAC_ISSUER = 'bff-gin-local-hmac';
const LOCAL_RSA_ISSUER = 'bff-gin-local-rsa';

function isLocalIssuer(iss) {
  return iss === LOCAL_HMAC_ISSUER || iss === LOCAL_RSA_ISSUER;
}

class VerifyError extends Error {}

// 署名検証前にissだけ覗く(誰でも書き換えられる値。ここでは振り分け先の決定にのみ使い、
// 実際の信頼は委譲先verifierの署名検証に委ねる)。backend-rustのDispatcher::verifyと同じ設計
function peekIssuer(token) {
  const parts = token.split('.');
  if (parts.length !== 3) throw new VerifyError('JWT形式が不正');
  let payloadJson;
  try {
    payloadJson = Buffer.from(parts[1], 'base64url').toString('utf8');
  } catch (e) {
    throw new VerifyError(`ペイロードのデコードに失敗: ${e.message}`);
  }
  let payload;
  try {
    payload = JSON.parse(payloadJson);
  } catch (e) {
    throw new VerifyError(`ペイロードのJSON解析に失敗: ${e.message}`);
  }
  if (!payload.iss) throw new VerifyError('issクレームが無い');
  return payload.iss;
}

// HmacVerifier はローカル認証HMAC版(iss=LOCAL_HMAC_ISSUER)の検証
class HmacVerifier {
  constructor(secret, issuer, audience) {
    this.secret = secret;
    this.issuer = issuer;
    this.audience = audience;
  }

  async verify(token) {
    try {
      return jwt.verify(token, this.secret, {
        algorithms: ['HS256'],
        issuer: this.issuer,
        audience: this.audience,
      });
    } catch (e) {
      throw new VerifyError(`HMAC: ${e.message}`);
    }
  }
}

// JwksVerifier はKeycloak/ローカルRSA共通のJWKSベース検証
// kid(鍵ID)ごとに公開鍵をキャッシュし、未知のkidが来たときだけJWKS再取得する
// (backend-rustのJwksVerifierと同じ「kid不一致時のみ再取得」戦略)
class JwksVerifier {
  constructor(jwksUrl, issuer, audience) {
    this.jwksUrl = jwksUrl;
    this.issuer = issuer;
    this.audience = audience;
    this.keys = new Map();
  }

  async refresh() {
    const resp = await fetch(this.jwksUrl);
    if (!resp.ok) throw new VerifyError(`JWKS取得失敗: HTTP ${resp.status}`);
    const parsed = await resp.json();
    const next = new Map();
    for (const k of parsed.keys || []) {
      if (k.kty !== 'RSA') continue;
      if (k.use && k.use !== 'sig') continue;
      try {
        const keyObject = crypto.createPublicKey({
          key: { kty: k.kty, n: k.n, e: k.e },
          format: 'jwk',
        });
        next.set(k.kid, keyObject);
      } catch (e) {
        // 不正な鍵はスキップする(backend-rustのJwksVerifier::refreshと同様)
      }
    }
    this.keys = next;
  }

  async verify(token) {
    let decoded;
    try {
      decoded = jwt.decode(token, { complete: true });
    } catch (e) {
      throw new VerifyError(`ヘッダ解析失敗: ${e.message}`);
    }
    const kid = decoded && decoded.header && decoded.header.kid;
    if (!kid) throw new VerifyError('JWTヘッダにkidが無い');

    let key = this.keys.get(kid);
    if (!key) {
      await this.refresh();
      key = this.keys.get(kid);
      if (!key) throw new VerifyError(`kid=${kid} に対応する公開鍵が見つからない`);
    }

    try {
      return jwt.verify(token, key, {
        algorithms: ['RS256'],
        issuer: this.issuer,
        audience: this.audience,
      });
    } catch (e) {
      throw new VerifyError(`RSA/JWKS: ${e.message}`);
    }
  }
}

// Dispatcher はJWTのissクレームで検証方式(Keycloak/ローカルHMAC/ローカルRSA)を振り分ける
// (backend-rustのDispatcherと同じ2段構造。issの詐称は委譲先の署名検証で弾かれる)
class Dispatcher {
  constructor() {
    this.byIssuer = new Map();
  }

  register(issuer, verifier) {
    this.byIssuer.set(issuer, verifier);
    return this;
  }

  async verify(token) {
    const iss = peekIssuer(token);
    const verifier = this.byIssuer.get(iss);
    if (!verifier) throw new VerifyError(`不明なissuer: ${iss}`);
    return verifier.verify(token);
  }
}

module.exports = {
  LOCAL_HMAC_ISSUER,
  LOCAL_RSA_ISSUER,
  isLocalIssuer,
  peekIssuer,
  VerifyError,
  HmacVerifier,
  JwksVerifier,
  Dispatcher,
};
