'use strict';

const express = require('express');
const task = require('./task');
const errors = require('../error');
const { requestLogger } = require('../logging');

function router(state) {
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

module.exports = { router };
