// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { expect, test } from 'bun:test';

process.env.NEW_RELIC_ENABLED = 'false';

const { RpcError } = await import('./nats');
const { thrownStatus } = await import('./form-action');

test.each([
  ['an invalid refusal', new RpcError('bad', 'invalid'), 400],
  ['a forbidden refusal', new RpcError('no', 'forbidden'), 403],
  ['a missing record', new RpcError('gone', 'not_found'), 404],
  ['a conflict', new RpcError('taken', 'conflict'), 409],
  ['an unavailable service', new RpcError('down', 'unavailable'), 502],
  ['an internal refusal', new RpcError('boom', 'internal'), 502],
  ['an uncoded RPC error', new RpcError('odd'), 502],
  ['a transport timeout', new Error('TIMEOUT'), 502],
  ['a non-error throw', 'nope', 502]
] as [string, unknown, number][])('%s answers %p', (_name, err, status) => {
  expect(thrownStatus(err)).toBe(status);
});
