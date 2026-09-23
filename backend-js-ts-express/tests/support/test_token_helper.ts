// backend-js-express/tests/support/test_token_helper.jsの型付き移植。ロジックは変更していない。

import jwt from 'jsonwebtoken';
import crypto from 'node:crypto';

export function makeHmacToken(
  secret: string,
  iss: string,
  aud: string,
  sub: string,
  expOffsetSecs: number,
  alg: jwt.Algorithm = 'HS256',
  azp?: string,
): string {
  const payload: Record<string, unknown> = {
    sub,
    iss,
    aud,
    exp: Math.floor(Date.now() / 1000) + expOffsetSecs,
  };
  if (azp !== undefined) payload.azp = azp;
  return jwt.sign(payload, secret, { algorithm: alg, noTimestamp: true });
}

// "alg":"none"は署名の要らない自己主張トークンになるため、必ず拒否されなければならない。
export function makeAlgNoneToken(iss: string, aud: string, sub: string, expOffsetSecs: number): string {
  const header = { alg: 'none', typ: 'JWT' };
  const payload = {
    sub,
    iss,
    aud,
    exp: Math.floor(Date.now() / 1000) + expOffsetSecs,
  };
  const b64 = (obj: unknown) => Buffer.from(JSON.stringify(obj)).toString('base64url');
  return `${b64(header)}.${b64(payload)}.`;
}

export interface RsaKeyPairPem {
  publicKey: string;
  privateKey: string;
}

export function generateRsaKeyPair(): RsaKeyPairPem {
  return crypto.generateKeyPairSync('rsa', {
    modulusLength: 2048,
    publicKeyEncoding: { type: 'spki', format: 'pem' },
    privateKeyEncoding: { type: 'pkcs8', format: 'pem' },
  });
}

export function makeRsaToken(
  privateKeyPem: string,
  kid: string,
  iss: string,
  aud: string,
  sub: string,
  expOffsetSecs: number,
  alg: jwt.Algorithm = 'RS256',
  azp?: string,
): string {
  const payload: Record<string, unknown> = {
    sub,
    iss,
    aud,
    exp: Math.floor(Date.now() / 1000) + expOffsetSecs,
  };
  if (azp !== undefined) payload.azp = azp;
  return jwt.sign(payload, privateKeyPem, { algorithm: alg, header: { kid, alg }, noTimestamp: true });
}
