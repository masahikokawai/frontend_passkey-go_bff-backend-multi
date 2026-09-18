// backend-js/src/grpc/index.jsの型付き移植。ロジックは変更していない。

import path from 'node:path';
import * as grpc from '@grpc/grpc-js';
import * as protoLoader from '@grpc/proto-loader';
import { buildService, withLogging } from './task';
import type { AppState } from '../types';

function loadProto(): grpc.GrpcObject {
  const protoPath = path.join(__dirname, '..', '..', 'proto', 'task', 'v1', 'task.proto');
  const packageDefinition = protoLoader.loadSync(protoPath, {
    keepCase: true,
    // 64bit整数フィールド(id等)をJS Numberとして扱う(backend-jsと同じ単純化、README参照)
    longs: Number,
    enums: String,
    defaults: true,
    oneofs: true,
  });
  const loaded = grpc.loadPackageDefinition(packageDefinition) as unknown as {
    task: { v1: grpc.GrpcObject };
  };
  return loaded.task.v1;
}

export function createServer(state: AppState): grpc.Server {
  const pkg = loadProto() as unknown as { TaskService: { service: grpc.ServiceDefinition } };
  const server = new grpc.Server();
  server.addService(pkg.TaskService.service, withLogging(buildService(state)));
  return server;
}
