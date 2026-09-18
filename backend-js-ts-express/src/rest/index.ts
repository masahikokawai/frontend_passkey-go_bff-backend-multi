// backend-js/src/rest/index.jsの型付き移植。ロジックは変更していない。

import express from 'express';
import * as task from './task';
import * as errors from '../error';
import { requestLogger } from '../logging';
import type { AppState } from '../types';

export function router(state: AppState) {
  const r = express.Router();
  r.use(express.json());
  r.use(requestLogger());

  r.get('/internal/v1/tasks', task.list(state));
  r.post('/internal/v1/tasks', task.create(state));
  r.get('/internal/v1/tasks/:id', task.get(state));
  r.patch('/internal/v1/tasks/:id', task.update(state));
  r.delete('/internal/v1/tasks/:id', task.remove(state));

  r.use(errors.errorHandler());
  return r;
}
