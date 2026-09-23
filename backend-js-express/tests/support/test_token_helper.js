'use strict';

// backend-java/backend-python の test_support/TestTokenHelper 相当。
// テスト専用のJWT生成ヘルパー(HMAC/RSA双方、alg混同攻撃テスト用のトークンも作れる)

const jwt = require('jsonwebtoken');
const crypto = require('node:crypto');

function makeHmacToken(secret, iss, aud, sub, expOffsetSecs, alg = 'HS256', azp) {
  const payload = {
    sub,
    iss,
    aud,
    exp: Math.floor(Date.now() / 1000) + expOffsetSecs,
  };
  if (azp !== undefined) payload.azp = azp;
  return jwt.sign(payload, secret, { algorithm: alg, noTimestamp: true });
}

// "alg":"none"は署名の要らない自己主張トークンになるため、必ず拒否されなければならない。
// jsonwebtokenのsign()にalg=noneを強制させるのは意図が伝わりにくいため、手動で組み立てる
// (backend-java/backend-pythonの同名ヘルパーと同じ意図)
function makeAlgNoneToken(iss, aud, sub, expOffsetSecs) {
  const header = { alg: 'none', typ: 'JWT' };
  const payload = {
    sub,
    iss,
    aud,
    exp: Math.floor(Date.now() / 1000) + expOffsetSecs,
  };
  const b64 = (obj) => Buffer.from(JSON.stringify(obj)).toString('base64url');
  return `${b64(header)}.${b64(payload)}.`;
}

function generateRsaKeyPair() {
  return crypto.generateKeyPairSync('rsa', {
    modulusLength: 2048,
    publicKeyEncoding: { type: 'spki', format: 'pem' },
    privateKeyEncoding: { type: 'pkcs8', format: 'pem' },
  });
}

function makeRsaToken(privateKeyPem, kid, iss, aud, sub, expOffsetSecs, alg = 'RS256', azp) {
  const payload = {
    sub,
    iss,
    aud,
    exp: Math.floor(Date.now() / 1000) + expOffsetSecs,
  };
  if (azp !== undefined) payload.azp = azp;
  return jwt.sign(payload, privateKeyPem, { algorithm: alg, header: { kid, alg }, noTimestamp: true });
}

module.exports = {
  makeHmacToken,
  makeAlgNoneToken,
  generateRsaKeyPair,
  makeRsaToken,
};
