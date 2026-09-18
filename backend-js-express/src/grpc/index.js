'use strict';

const path = require('node:path');
const grpc = require('@grpc/grpc-js');
const protoLoader = require('@grpc/proto-loader');
const { buildService, withLogging } = require('./task');

function loadProto() {
  const protoPath = path.join(__dirname, '..', '..', 'proto', 'task', 'v1', 'task.proto');
  const packageDefinition = protoLoader.loadSync(protoPath, {
    keepCase: true,
    // 64bit整数フィールド(id等)をJS Numberとして扱う。このプロジェクトの規模では
    // Number.MAX_SAFE_INTEGERを超えるidは発生しない前提の単純化(README参照)
    longs: Number,
    enums: String,
    defaults: true,
    oneofs: true,
  });
  return grpc.loadPackageDefinition(packageDefinition).task.v1;
}

function createServer(state) {
  const pkg = loadProto();
  const server = new grpc.Server();
  server.addService(pkg.TaskService.service, withLogging(buildService(state)));
  return server;
}

module.exports = { createServer };
