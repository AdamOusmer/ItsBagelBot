// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { afterAll, beforeAll, expect, mock, test } from 'bun:test';
import { spawn, spawnSync } from 'node:child_process';
import { createServer } from 'node:net';
import Redis from 'iovalkey';
import { VALKEY_CLIENT_OPTIONS } from '@bagel/kit/server/valkey-connection';

process.env.NEW_RELIC_ENABLED = 'false';
mock.module('$app/env', () => ({ dev: false }));

const { createOverlayHub, overlayStreamResponse, overlaySubscriberOn } = await import('./overlay-hub');

const valkeyServer = spawnSync('which', ['valkey-server']).status === 0;
const testAddr = process.env.VALKEY_TEST_ADDR;
const runnable = valkeyServer || !!testAddr;

function freePort(): Promise<number> {
  return new Promise((resolve) => {
    const probe = createServer();
    probe.listen(0, '127.0.0.1', () => {
      const { port } = probe.address() as { port: number };
      probe.close(() => resolve(port));
    });
  });
}

let server: ReturnType<typeof spawn> | undefined;
let base: Redis;

async function until(check: () => boolean | Promise<boolean>, ms = 3000): Promise<void> {
  const deadline = Date.now() + ms;
  while (!(await check())) {
    if (Date.now() > deadline) throw new Error('timed out');
    await Bun.sleep(10);
  }
}

beforeAll(async () => {
  if (!runnable) return;
  let host = '127.0.0.1';
  let port: number;
  if (testAddr) {
    [host, port] = [testAddr.split(':')[0]!, Number(testAddr.split(':')[1])];
  } else {
    port = await freePort();
    server = spawn('valkey-server', ['--port', String(port), '--save', '', '--appendonly', 'no'], { stdio: 'ignore' });
  }
  base = new Redis({ host, port, ...VALKEY_CLIENT_OPTIONS });
  base.on('error', () => {});
  await until(() => base.status === 'ready');
});

afterAll(() => {
  base?.disconnect();
  server?.kill();
});

const channel = `overlay:${process.pid}`;
const channelId = String(process.pid);
const item = (content: string) => JSON.stringify({ v: 1, kind: 'text', content, seconds: 1, position: 'top' });

test.skipIf(!runnable)('live subscribe, PUBLISH count, resubscribe after a killed connection, release on abort', async () => {
  const hub = createOverlayHub({
    connect: (events) => overlaySubscriberOn(base, events),
    stillValid: () => Promise.resolve(true)
  });
  const abort = new AbortController();
  const res = overlayStreamResponse(new Request('http://x/stream', { signal: abort.signal }), hub, channelId, 'h');
  const reader = res.body!.getReader();
  const dec = new TextDecoder();
  await reader.read();
  await reader.read();

  await until(async () => (await base.publish(channel, item('first'))) === 1);
  expect(dec.decode((await reader.read()).value)).toContain('"first"');

  expect(await base.publish(`overlay:${process.pid}0`, item('other'))).toBe(0);

  await base.call('CLIENT', 'KILL', 'TYPE', 'pubsub');
  await until(async () => (await base.publish(channel, item('again'))) === 1);
  let frame = '';
  while (!frame.includes('"again"')) frame = dec.decode((await reader.read()).value);

  abort.abort();
  expect(hub.size()).toEqual({ channels: 0, streams: 0, connected: false });
  await until(async () => (await base.publish(channel, item('gone'))) === 0);
});
