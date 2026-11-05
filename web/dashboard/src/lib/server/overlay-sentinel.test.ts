// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { afterAll, beforeAll, expect, mock, test } from 'bun:test';
import { spawn, spawnSync } from 'node:child_process';
import { mkdtempSync, rmSync, writeFileSync } from 'node:fs';
import { createServer } from 'node:net';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import Redis from 'iovalkey';
import { VALKEY_CLIENT_OPTIONS } from '@bagel/kit/server/valkey-connection';

process.env.NEW_RELIC_ENABLED = 'false';
mock.module('$app/env', () => ({ dev: false }));

const { createOverlayHub, overlayStreamResponse, overlaySubscriberOn } = await import('./overlay-hub');

const runnable = ['valkey-server', 'valkey-sentinel'].every((bin) => spawnSync('which', [bin]).status === 0);

const dir = runnable ? mkdtempSync(join(tmpdir(), 'overlay-sentinel-')) : '';
const processes: ReturnType<typeof spawn>[] = [];
const admins: Redis[] = [];
let base: Redis;
let sentinel: Redis;

function freePort(): Promise<number> {
  return new Promise((resolve) => {
    const probe = createServer();
    probe.listen(0, '127.0.0.1', () => {
      const { port } = probe.address() as { port: number };
      probe.close(() => resolve(port));
    });
  });
}

async function until(check: () => boolean | Promise<boolean>, ms: number): Promise<void> {
  const deadline = Date.now() + ms;
  while (!(await check())) {
    if (Date.now() > deadline) throw new Error('timed out');
    await Bun.sleep(25);
  }
}

function admin(port: number): Redis {
  const client = new Redis({ host: '127.0.0.1', port, maxRetriesPerRequest: 1 });
  client.on('error', () => {});
  admins.push(client);
  return client;
}

async function primaryPort(): Promise<number> {
  const [, port] = (await sentinel.call('SENTINEL', 'get-master-addr-by-name', 'myprimary')) as string[];
  return Number(port);
}

beforeAll(async () => {
  if (!runnable) return;
  const [first, second, sentinelPort] = [await freePort(), await freePort(), await freePort()];
  processes.push(spawn('valkey-server', ['--port', String(first), '--save', '', '--dir', dir], { stdio: 'ignore' }));
  processes.push(spawn('valkey-server', ['--port', String(second), '--save', '', '--dir', dir, '--dbfilename', 'b.rdb', '--replicaof', `127.0.0.1 ${first}`], { stdio: 'ignore' }));
  const conf = join(dir, 'sentinel.conf');
  writeFileSync(conf, [`port ${sentinelPort}`, `sentinel monitor myprimary 127.0.0.1 ${first} 1`, 'sentinel down-after-milliseconds myprimary 5000', 'sentinel failover-timeout myprimary 60000'].join('\n'));
  processes.push(spawn('valkey-sentinel', [conf], { stdio: 'ignore' }));
  sentinel = admin(sentinelPort);
  await until(async () => {
    const replicas = (await sentinel.call('SENTINEL', 'replicas', 'myprimary').catch(() => [])) as string[][];
    return replicas.length === 1 && replicas[0]!.join(' ').includes('master-link-status ok');
  }, 20000);
  base = new Redis({ sentinels: [{ host: '127.0.0.1', port: sentinelPort }], name: 'myprimary', ...VALKEY_CLIENT_OPTIONS });
  base.on('error', () => {});
  await until(() => base.status === 'ready', 5000);
}, 30000);

afterAll(() => {
  base?.disconnect();
  for (const client of admins) client.disconnect();
  for (const child of processes) child.kill('SIGKILL');
  if (dir) rmSync(dir, { recursive: true, force: true });
});

const channelId = String(process.pid);
const channel = `overlay:${channelId}`;
const item = (content: string) => JSON.stringify({ v: 1, kind: 'text', content, seconds: 1, position: 'top' });

test.skipIf(!runnable)(
  'a failover moves the one subscription to the new primary before Sentinel kills the old connection',
  async () => {
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

    const before = await primaryPort();
    const previous = admin(before);
    await until(async () => (await previous.publish(channel, item('before'))) === 1, 5000);

    await until(async () => (await sentinel.call('SENTINEL', 'failover', 'myprimary').catch(() => '')) === 'OK', 20000);
    const started = Date.now();
    await until(async () => (await primaryPort()) !== before, 15000);
    const after = await primaryPort();
    const next = admin(after);

    await until(async () => (await next.publish(channel, item('after')).catch(() => 0)) === 1, 15000);
    const resumedMs = Date.now() - started;

    let frame = '';
    while (!frame.includes('"after"')) frame = dec.decode((await reader.read()).value);
    const [, subscribers] = (await next.call('PUBSUB', 'NUMSUB', channel)) as [string, number];
    expect(Number(subscribers)).toBe(1);
    expect(resumedMs).toBeLessThan(8000);

    abort.abort();
    expect(hub.size()).toEqual({ channels: 0, streams: 0, connected: false });
  },
  90000
);
