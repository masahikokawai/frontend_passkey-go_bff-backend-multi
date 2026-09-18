// backend-js/src/external/index.jsの型付き移植。ロジックは変更していない。

import express from 'express';
import * as task from './task';
import * as errors from '../error';
import { requestLogger } from '../logging';
import type { ExternalState } from '../types';

export function router(state: ExternalState) {
  const r = express.Router();
  r.use(requestLogger());
  r.get('/external/v1/tasks', task.list(state));
  r.use(errors.errorHandler());
  return r;
}
