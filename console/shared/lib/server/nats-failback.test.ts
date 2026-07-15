import { afterAll, describe, expect, mock, test } from 'bun:test';

mock.module('newrelic', () => ({
  default: {
    startSegment: (_name: string, _record: boolean, run: () => unknown) => run(),
    recordMetric: () => {}
  }
}));
const { hubJetStreamOptions, localLeafReady } = await import('./nats');

const server = Bun.serve({
  port: 0,
  fetch(request) {
    const path = new URL(request.url).pathname;
    return path === '/healthz' ? new Response('ok') : new Response('missing', { status: 404 });
  }
});

afterAll(() => server.stop(true));

describe('local leaf failback probe', () => {
  test('accepts the monitor health endpoint', async () => {
    expect(await localLeafReady(`${server.url}healthz`, 500)).toBe(true);
  });

  test('rejects non-200 and unreachable endpoints', async () => {
    expect(await localLeafReady(`${server.url}missing`, 500)).toBe(false);
    expect(await localLeafReady('http://127.0.0.1:1/healthz', 25)).toBe(false);
  });
});

describe('direct-hub JetStream options', () => {
  test('uses the API prefix authorized on the hub account', () => {
    expect(hubJetStreamOptions()).toEqual({ apiPrefix: '$JS.API', checkAPI: false });
  });

  test('returns a fresh object for client normalization', () => {
    expect(hubJetStreamOptions()).not.toBe(hubJetStreamOptions());
  });
});
