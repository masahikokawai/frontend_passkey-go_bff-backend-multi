'use strict';

// backend-java/backend-pythonのMockJwksServer相当。
// 自プロセス内蔵の使い捨てHTTPサーバーで/.well-known/jwks.json形式のレスポンスを返す。
// 本番はKeycloak/bff自身のJWKSエンドポイントを叩くが、JwksVerifierからすれば
// 「HTTPでJWKS JSONを返す何か」であればよいので、テストではnode:httpだけで十分。

const http = require('node:http');
const crypto = require('node:crypto');

class MockJwksServer {
  constructor() {
    this._keys = []; // [{ kid, jwk }]
    this._server = http.createServer((req, res) => {
      const body = JSON.stringify({
        keys: this._keys.map(({ kid, jwk }) => ({
          kty: 'RSA',
          kid,
          use: 'sig',
          n: jwk.n,
          e: jwk.e,
        })),
      });
      res.writeHead(200, { 'Content-Type': 'application/json' });
      res.end(body);
    });
  }

  async start() {
    await new Promise((resolve) => this._server.listen(0, '127.0.0.1', resolve));
    return this;
  }

  // publicKeyPem: crypto.generateKeyPairSync('rsa', ...).publicKey (PEM文字列)
  addKey(kid, publicKeyPem) {
    const jwk = crypto.createPublicKey(publicKeyPem).export({ format: 'jwk' });
    this._keys.push({ kid, jwk });
  }

  jwksUrl() {
    const { port } = this._server.address();
    return `http://127.0.0.1:${port}/jwks`;
  }

  async close() {
    await new Promise((resolve, reject) => {
      this._server.close((err) => (err ? reject(err) : resolve()));
    });
  }
}

async function createMockJwksServer() {
  const server = new MockJwksServer();
  await server.start();
  return server;
}

module.exports = { MockJwksServer, createMockJwksServer };
