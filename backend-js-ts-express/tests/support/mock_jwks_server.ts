// backend-js-express/tests/support/mock_jwks_server.jsの型付き移植。ロジックは変更していない。

import http from 'node:http';
import crypto from 'node:crypto';

interface StoredKey {
  kid: string;
  jwk: crypto.JsonWebKey;
}

export class MockJwksServer {
  private keys: StoredKey[] = [];
  private server: http.Server;

  constructor() {
    this.server = http.createServer((_req, res) => {
      const body = JSON.stringify({
        keys: this.keys.map(({ kid, jwk }) => ({
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

  async start(): Promise<this> {
    await new Promise<void>((resolve) => this.server.listen(0, '127.0.0.1', resolve));
    return this;
  }

  // publicKeyPem: crypto.generateKeyPairSync('rsa', ...).publicKey (PEM文字列)
  addKey(kid: string, publicKeyPem: string): void {
    const jwk = crypto.createPublicKey(publicKeyPem).export({ format: 'jwk' }) as crypto.JsonWebKey;
    this.keys.push({ kid, jwk });
  }

  jwksUrl(): string {
    const address = this.server.address();
    const port = typeof address === 'object' && address ? address.port : 0;
    return `http://127.0.0.1:${port}/jwks`;
  }

  async close(): Promise<void> {
    await new Promise<void>((resolve, reject) => {
      this.server.close((err) => (err ? reject(err) : resolve()));
    });
  }
}

export async function createMockJwksServer(): Promise<MockJwksServer> {
  const server = new MockJwksServer();
  await server.start();
  return server;
}
