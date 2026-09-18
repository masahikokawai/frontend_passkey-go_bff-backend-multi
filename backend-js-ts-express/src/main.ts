// backend-js/src/main.jsの型付き移植。ロジックは変更していない。

import express from 'express';
import * as grpcLib from '@grpc/grpc-js';

import { loadConfig } from './config';
import * as db from './db';
import { Dispatcher, HmacVerifier, JwksVerifier, LOCAL_HMAC_ISSUER, LOCAL_RSA_ISSUER } from './auth/jwt';
import { FlagCache } from './flags';
import * as restModule from './rest';
import * as externalModule from './external';
import * as grpcModule from './grpc';
import { logInfo, logWarn } from './logging';
import type { AppState, ExternalState } from './types';

// Go実装の既定値は":8103"のような形式(host省略)。ExpressのlistenやgRPCのbindAsyncには
// host:portの形が必要なため、先頭が':'なら0.0.0.0を補う
function parseAddr(addr: string): { host: string; port: number } {
  if (addr.startsWith(':')) {
    return { host: '0.0.0.0', port: parseInt(addr.slice(1), 10) };
  }
  const idx = addr.lastIndexOf(':');
  return { host: addr.slice(0, idx), port: parseInt(addr.slice(idx + 1), 10) };
}

async function main(): Promise<void> {
  const cfg = loadConfig();

  logInfo('MySQLへ接続します', { dsn: cfg.dbDsn });
  const pool = db.connect(cfg.dbDsn);

  // backend(Go)のcmd/server/main.goと同じ3issuer構成
  // (Keycloak/ローカルHMAC/ローカルRSA)をDispatcherへ登録する
  const keycloakVerifier = new JwksVerifier(cfg.keycloakJwksUrl, cfg.keycloakIssuer, cfg.expectedAudience);
  const localHmacVerifier = new HmacVerifier(cfg.localHmacSecret, LOCAL_HMAC_ISSUER, cfg.expectedAudience);
  const localRsaVerifier = new JwksVerifier(cfg.localRsaJwksUrl, LOCAL_RSA_ISSUER, cfg.expectedAudience);

  const dispatcher = new Dispatcher()
    .register(cfg.keycloakIssuer, keycloakVerifier)
    .register(LOCAL_HMAC_ISSUER, localHmacVerifier)
    .register(LOCAL_RSA_ISSUER, localRsaVerifier);

  const state: AppState = { pool, dispatcher };

  // 外部公開API(CONTRACT.mdセクション11)専用: Keycloak発行のClient Credentials Grantトークンしか
  // 扱わないため、内部CRUD用の3issuer Dispatcherとは別にKeycloak向けのJwksVerifier単体を用意する
  const externalKeycloakVerifier = new JwksVerifier(cfg.keycloakJwksUrl, cfg.keycloakIssuer, cfg.expectedAudience);
  const paginationV2Flag = await FlagCache.spawn(
    pool,
    'backend.external-tasks-pagination-v2',
    cfg.featureFlagPollIntervalSecs * 1000,
  );
  const externalState: ExternalState = {
    pool,
    keycloakVerifier: externalKeycloakVerifier,
    expectedClientId: cfg.externalApiClientId,
    paginationV2Flag,
  };

  const restAddr = parseAddr(cfg.httpAddr);
  const restApp = express();
  restApp.use(restModule.router(state));
  restApp.listen(restAddr.port, restAddr.host, () => {
    logInfo('REST(Express)起動', { addr: cfg.httpAddr });
  });

  const externalAddr = parseAddr(cfg.externalHttpAddr);
  const externalApp = express();
  externalApp.use(externalModule.router(externalState));
  externalApp.listen(externalAddr.port, externalAddr.host, () => {
    logInfo('外部公開API(Express)起動', { addr: cfg.externalHttpAddr });
  });

  const grpcAddr = parseAddr(cfg.grpcAddr);
  const grpcServer = grpcModule.createServer(state);
  grpcServer.bindAsync(`${grpcAddr.host}:${grpcAddr.port}`, grpcLib.ServerCredentials.createInsecure(), (err) => {
    if (err) {
      logWarn('gRPC起動に失敗', { error: err.message });
      throw err;
    }
    logInfo('gRPC(@grpc/grpc-js)起動', { addr: cfg.grpcAddr });
  });
}

main().catch((err) => {
  // eslint-disable-next-line no-console
  console.error('起動に失敗しました', err);
  process.exit(1);
});
