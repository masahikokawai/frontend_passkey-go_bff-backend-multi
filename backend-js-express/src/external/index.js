'use strict';

const express = require('express');
const task = require('./task');
const errors = require('../error');
const { requestLogger } = require('../logging');

function router(state) {
  const r = express.Router();
  r.use(requestLogger());
  r.get('/external/v1/tasks', task.list(state));
  r.use(errors.errorHandler());
  return r;
}

module.exports = { router };
