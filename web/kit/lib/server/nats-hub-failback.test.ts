// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { createServer, type Server, type Socket } from 'node:net';
import { afterEach, beforeEach, describe, expect, mock, test } from 'bun:test';
import { connect } from '@nats-io/transport-node';

mock.module('newrelic', () => ({
  default: {
    startSegment: (_name: string, _record: boolean, run: () => unknown) => run(),
    recordMetric: () => {}
  }
}));
const { FailbackStreak, enableHubFailback, probeHubName, resetLocalHubProbe } = await import('./nats');

interface FakeHub {
  url: string;
  close: () => void;
}

// Answers the NATS handshake under a fixed server name, like a hub behind its Service.
async function fakeHub(serverName: string): Promise<FakeHub> {
  const sockets = new Set<Socket>();
  const server: Server = createServer((socket) => {
    sockets.add(socket);
    socket.on('close', () => sockets.delete(socket));
    socket.on('error', () => {});
    socket.on('data', (data) => {
      if (data.toString().includes('PING')) socket.write('PONG\r\n');
    });
    socket.write(
      `INFO ${JSON.stringify({ server_id: serverName, server_name: serverName, version: '2.11.0', proto: 1, headers: true, max_payload: 1048576 })}\r\n`
    );
  });
  await new Promise<void>((resolve) => server.listen(0, '127.0.0.1', resolve));
  const port = (server.address() as { port: number }).port;
  return {
    url: `nats://127.0.0.1:${port}`,
    close: () => {
      for (const socket of sockets) socket.destroy();
      server.close();
    }
  };
}

const sleep = (ms: number) => new Promise((resolve) => setTimeout(resolve, ms));

describe('FailbackStreak', () => {
  test('needs the full streak of displaced checks before a failback', () => {
    const streak = new FailbackStreak(2);
    expect(streak.observe('displaced')).toBe(false);
    expect(streak.observe('unknown')).toBe(false);
    expect(streak.observe('displaced')).toBe(false);
    expect(streak.observe('displaced')).toBe(true);
  });

  test('a failback that lands remote again doubles the streak, capped at 16x', () => {
    const streak = new FailbackStreak(1);
    const ticks: number[] = [];
    for (let tick = 1; tick <= 80; tick++) if (streak.observe('displaced')) ticks.push(tick);
    expect(ticks).toEqual([1, 3, 7, 15, 31, 47, 63, 79]);
  });

  test('landing local resets the backoff', () => {
    const streak = new FailbackStreak(1);
    expect(streak.observe('displaced')).toBe(true);
    expect(streak.observe('local')).toBe(false);
    expect(streak.observe('displaced')).toBe(true);
  });
});

describe('hub failback', () => {
  const ENV_KEYS = [
    'NODE_NAME',
    'NATS_HUB_URL',
    'NATS_LOCAL_HUB_URL',
    'NATS_FAILBACK_INTERVAL_MS',
    'NATS_FAILBACK_SUCCESSES',
    'NATS_FAILBACK_PROBE_TIMEOUT_MS'
  ];
  const saved: Record<string, string | undefined> = {};
  let remote: FakeHub;
  let local: FakeHub;

  beforeEach(async () => {
    for (const key of ENV_KEYS) saved[key] = process.env[key];
    remote = await fakeHub('nats-0');
    local = await fakeHub('nats-1');
    resetLocalHubProbe();
    Object.assign(process.env, {
      NODE_NAME: 'node1',
      NATS_HUB_URL: 'set',
      NATS_LOCAL_HUB_URL: local.url,
      NATS_FAILBACK_INTERVAL_MS: '30',
      NATS_FAILBACK_SUCCESSES: '2',
      NATS_FAILBACK_PROBE_TIMEOUT_MS: '200'
    });
  });

  afterEach(() => {
    for (const key of ENV_KEYS) {
      if (saved[key] === undefined) delete process.env[key];
      else process.env[key] = saved[key];
    }
    remote.close();
    local.close();
  });

  test('the probe reads the node-local hub name from the handshake', async () => {
    expect(await probeHubName({}, local.url, 200)).toBe('nats-1');
    expect(await probeHubName({}, 'nats://127.0.0.1:1', 200)).toBe('');
  });

  test.each([
    ['a connection on a remote hub fails back', 'remote', {}, true],
    ['a connection already on the local hub stays put', 'local', {}, false],
    ['an unreachable local hub keeps the connection', 'remote', { NATS_LOCAL_HUB_URL: 'nats://127.0.0.1:1' }, false],
    ['without a node name the hub plane never fails back', 'remote', { NODE_NAME: '' }, false],
    ['a connection that does not dial the hub never fails back', 'remote', { NATS_HUB_URL: '' }, false]
  ] as const)('%s', async (_name, target, env, reconnects) => {
    Object.assign(process.env, env);
    const hub = target === 'remote' ? remote : local;
    const nc = await connect({ servers: hub.url, reconnectTimeWait: 10 });
    try {
      let reconnected = false;
      void (async () => {
        for await (const status of nc.status()) if (status.type === 'reconnect') reconnected = true;
      })();
      enableHubFailback(nc, {});
      await sleep(400);
      expect(reconnected).toBe(reconnects);
    } finally {
      await nc.close();
    }
  });
});
