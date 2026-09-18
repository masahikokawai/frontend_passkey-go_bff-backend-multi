// backend-js/src/auth/jwt.jsの型付き移植。ロジックは変更していない。

import jwt from 'jsonwebtoken';
import crypto from 'node:crypto';
import type { JwtClaims } from '../types';

export const LOCAL_HMAC_ISSUER = 'bff-gin-local-hmac';
export const LOCAL_RSA_ISSUER = 'bff-gin-local-rsa';

export function isLocalIssuer(iss: string): boolean {
  return iss === LOCAL_HMAC_ISSUER || iss === LOCAL_RSA_ISSUER;
}

export class VerifyError extends Error {}

// 署名検証前にissだけ覗く(誰でも書き換えられる値。ここでは振り分け先の決定にのみ使い、
// 実際の信頼は委譲先verifierの署名検証に委ねる)。backend-rustのDispatcher::verifyと同じ設計
export function peekIssuer(token: string): string {
  const parts = token.split('.');
  if (parts.length !== 3) throw new VerifyError('JWT形式が不正');
  let payloadJson: string;
  try {
    payloadJson = Buffer.from(parts[1]!, 'base64url').toString('utf8');
  } catch (e) {
    throw new VerifyError(`ペイロードのデコードに失敗: ${(e as Error).message}`);
  }
  let payload: Record<string, unknown>;
  try {
    payload = JSON.parse(payloadJson);
  } catch (e) {
    throw new VerifyError(`ペイロードのJSON解析に失敗: ${(e as Error).message}`);
  }
  if (!payload.iss) throw new VerifyError('issクレームが無い');
  return payload.iss as string;
}

export interface Verifier {
  verify(token: string): Promise<JwtClaims>;
}

// HmacVerifier はローカル認証HMAC版(iss=LOCAL_HMAC_ISSUER)の検証
export class HmacVerifier implements Verifier {
  constructor(
    private readonly secret: string,
    private readonly issuer: string,
    private readonly audience: string,
  ) {}

  async verify(token: string): Promise<JwtClaims> {
    try {
      return jwt.verify(token, this.secret, {
        algorithms: ['HS256'],
        issuer: this.issuer,
        audience: this.audience,
      }) as JwtClaims;
    } catch (e) {
      throw new VerifyError(`HMAC: ${(e as Error).message}`);
    }
  }
}

// JwksVerifier はKeycloak/ローカルRSA共通のJWKSベース検証
// kid(鍵ID)ごとに公開鍵をキャッシュし、未知のkidが来たときだけJWKS再取得する
export class JwksVerifier implements Verifier {
  private keys = new Map<string, crypto.KeyObject>();

  constructor(
    private readonly jwksUrl: string,
    private readonly issuer: string,
    private readonly audience: string,
  ) {}

  async refresh(): Promise<void> {
    const resp = await fetch(this.jwksUrl);
    if (!resp.ok) throw new VerifyError(`JWKS取得失敗: HTTP ${resp.status}`);
    const parsed = (await resp.json()) as { keys?: Array<Record<string, string>> };
    const next = new Map<string, crypto.KeyObject>();
    for (const k of parsed.keys || []) {
      if (k.kty !== 'RSA') continue;
      if (k.use && k.use !== 'sig') continue;
      try {
        const keyObject = crypto.createPublicKey({
          key: { kty: k.kty, n: k.n, e: k.e } as unknown as crypto.JsonWebKeyInput['key'],
          format: 'jwk',
        });
        if (k.kid) next.set(k.kid, keyObject);
      } catch {
        // 不正な鍵はスキップする(backend-rust/backend-jsのJwksVerifier::refreshと同様)
      }
    }
    this.keys = next;
  }

  async verify(token: string): Promise<JwtClaims> {
    let decoded: ReturnType<typeof jwt.decode>;
    try {
      decoded = jwt.decode(token, { complete: true });
    } catch (e) {
      throw new VerifyError(`ヘッダ解析失敗: ${(e as Error).message}`);
    }
    const kid = decoded && typeof decoded === 'object' && 'header' in decoded ? decoded.header.kid : undefined;
    if (!kid) throw new VerifyError('JWTヘッダにkidが無い');

    let key = this.keys.get(kid);
    if (!key) {
      await this.refresh();
      key = this.keys.get(kid);
      if (!key) throw new VerifyError(`kid=${kid} に対応する公開鍵が見つからない`);
    }

    try {
      return jwt.verify(token, key as unknown as string, {
        algorithms: ['RS256'],
        issuer: this.issuer,
        audience: this.audience,
      }) as JwtClaims;
    } catch (e) {
      throw new VerifyError(`RSA/JWKS: ${(e as Error).message}`);
    }
  }
}

// Dispatcher はJWTのissクレームで検証方式(Keycloak/ローカルHMAC/ローカルRSA)を振り分ける
export class Dispatcher {
  private byIssuer = new Map<string, Verifier>();

  register(issuer: string, verifier: Verifier): this {
    this.byIssuer.set(issuer, verifier);
    return this;
  }

  async verify(token: string): Promise<JwtClaims> {
    const iss = peekIssuer(token);
    const verifier = this.byIssuer.get(iss);
    if (!verifier) throw new VerifyError(`不明なissuer: ${iss}`);
    return verifier.verify(token);
  }
}
