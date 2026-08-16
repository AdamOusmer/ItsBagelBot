// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import assert from 'node:assert/strict';
import { describe, test } from 'node:test';
import {
  CONNECTION_POLL_FAST_MS,
  connectionPollDelay,
  connectionPollSettled
} from './connection-poll';

describe('connection polling', () => {
  test('polls quickly through the ordinary reconnect window, then backs off', () => {
    assert.equal(connectionPollDelay(0), CONNECTION_POLL_FAST_MS);
    assert.equal(connectionPollDelay(4999), CONNECTION_POLL_FAST_MS);
    assert.equal(connectionPollDelay(5000), 2500);
  });

  test('connect waits for a transition or a short old-state grace period', () => {
    assert.equal(connectionPollSettled('connected', 'ok', 500, false), false);
    assert.equal(connectionPollSettled('connected', 'ok', 500, true), true);
    assert.equal(connectionPollSettled('connected', 'ok', 1500, false), true);
    assert.equal(connectionPollSettled('connected', 'pending', 5000, true), false);
  });

  test('disconnect waits for an inactive subscription state', () => {
    assert.equal(connectionPollSettled('disconnected', 'ok', 5000, true), false);
    assert.equal(connectionPollSettled('disconnected', 'unenrolled', 500, false), true);
    assert.equal(connectionPollSettled('disconnected', 'failing', 500, false), true);
    assert.equal(connectionPollSettled('disconnected', 'revoked', 500, false), true);
  });
});
