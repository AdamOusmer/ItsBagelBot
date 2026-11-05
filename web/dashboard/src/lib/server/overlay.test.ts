// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { afterEach, beforeEach, describe, expect, jest, mock, test } from 'bun:test';
import { readFileSync } from 'node:fs';
import { join } from 'node:path';

process.env.NEW_RELIC_ENABLED = 'false';
mock.module('$app/env', () => ({ dev: false }));

const {
  createOverlayHub,
  overlayStreamResponse
} = await import('./overlay-hub');
const { OVERLAY_CSP, OVERLAY_SCRIPT, overlayNotFound, overlayPageResponse } = await import('./overlay-page');
const {
  hashOverlayToken,
  overlayConfigured,
  overlayHashMatches,
  rotateOverlayToken,
  stillValidOverlayHash,
  verifyOverlayToken
} = await import('./overlay-token');
const { OVERLAY_REVOKE_MESSAGE, overlayItemFrame, overlayMediaUrl, readOverlayMessage } = await import('./overlay-wire');

type Hub = ReturnType<typeof createOverlayHub>;
type Events = Parameters<Parameters<typeof createOverlayHub>[0]['connect']>[0];

const TEXT = JSON.stringify({ v: 1, kind: 'text', content: 'hi', seconds: 5, position: 'top' });

function memoryStore() {
  const hashes = new Map<string, string>();
  const revoked: string[] = [];
  return {
    hashes,
    revoked,
    reads: 0,
    readHash(channelId: string) {
      this.reads += 1;
      return Promise.resolve(hashes.get(channelId) ?? null);
    },
    writeHash(channelId: string, hash: string) {
      hashes.set(channelId, hash);
      return Promise.resolve();
    },
    announceRevoked(channelId: string) {
      revoked.push(channelId);
      return Promise.resolve();
    }
  };
}

function fakeBus() {
  const subscribed = new Set<string>();
  const state = { events: null as Events | null, connects: 0, closes: 0, isReady: true };
  return {
    subscribed,
    state,
    connect(events: Events) {
      state.events = events;
      state.connects += 1;
      return {
        subscribe(channel: string) {
          subscribed.add(channel);
          return Promise.resolve();
        },
        unsubscribe(channel: string) {
          subscribed.delete(channel);
          return Promise.resolve();
        },
        ready: () => state.isReady,
        close() {
          state.closes += 1;
        }
      };
    },
    publish(channel: string, message: string) {
      if (subscribed.has(channel)) state.events?.onMessage(channel, message);
    }
  };
}

function hubOn(bus = fakeBus(), stillValid: (channelId: string, hash: string) => Promise<boolean> = () => Promise.resolve(true)) {
  return { bus, hub: createOverlayHub({ connect: (events) => bus.connect(events), stillValid }) };
}

function open(hub: Hub, channelId = '1', tokenHash = 'h') {
  const abort = new AbortController();
  const res = overlayStreamResponse(new Request('http://x/stream', { signal: abort.signal }), hub, channelId, tokenHash);
  return { abort, res, reader: res.body!.getReader() };
}

const dec = new TextDecoder();

async function readFrames(reader: ReadableStreamDefaultReader<Uint8Array>, count: number): Promise<string[]> {
  const frames: string[] = [];
  for (let i = 0; i < count; i++) {
    const { value, done } = await reader.read();
    if (done) break;
    frames.push(dec.decode(value));
  }
  return frames;
}

describe('overlay token', () => {
  test('a minted token verifies and stores only its hash, durable across a fresh process read', async () => {
    const store = memoryStore();
    const token = await rotateOverlayToken('42', store);
    expect(token).toMatch(/^42\.[\w-]{43}$/);
    expect(Buffer.from(token.split('.')[1]!, 'base64url')).toHaveLength(32);
    expect(store.hashes.get('42')).toBe(hashOverlayToken(token));
    expect(JSON.stringify([...store.hashes])).not.toContain(token.split('.')[1]);
    expect(await verifyOverlayToken(token, store)).toEqual({ channelId: '42', tokenHash: hashOverlayToken(token) });
    expect(await overlayConfigured('42', store)).toBe(true);
    expect(await overlayConfigured('43', store)).toBe(false);
  });

  test('a wrong token, another channel and a missing channel all fail the same compare', async () => {
    const store = memoryStore();
    const token = await rotateOverlayToken('42', store);
    const secret = token.split('.')[1]!;
    const wrong = `42.${secret.slice(0, -1)}${secret.endsWith('A') ? 'B' : 'A'}`;
    expect(await verifyOverlayToken(wrong, store)).toBeNull();
    expect(await verifyOverlayToken(`43.${secret}`, store)).toBeNull();
    expect(overlayHashMatches(hashOverlayToken(wrong), null)).toBe(false);
    expect(overlayHashMatches(hashOverlayToken(wrong), 'not-a-hash')).toBe(false);
  });

  test('a malformed token never touches the store', async () => {
    const store = memoryStore();
    for (const bad of ['', 'abc', '42', '42.short', 'x.'.padEnd(45, 'a'), `42.${'a'.repeat(44)}`, `42.${'<'.repeat(43)}`, `../42.${'a'.repeat(43)}`]) {
      expect(await verifyOverlayToken(bad, store)).toBeNull();
    }
    expect(store.reads).toBe(0);
  });

  test('rotating invalidates the old token and announces the revoke', async () => {
    const store = memoryStore();
    const first = await rotateOverlayToken('42', store);
    const second = await rotateOverlayToken('42', store);
    expect(second).not.toBe(first);
    expect(await verifyOverlayToken(first, store)).toBeNull();
    expect(await verifyOverlayToken(second, store)).not.toBeNull();
    expect(await stillValidOverlayHash('42', hashOverlayToken(first), store)).toBe(false);
    expect(store.revoked).toEqual(['42', '42']);
  });

  test('a failed revoke announcement does not undo the rotation', async () => {
    const store = { ...memoryStore(), announceRevoked: () => Promise.reject(new Error('NOPERM')) };
    const token = await rotateOverlayToken('42', store);
    expect(await verifyOverlayToken(token, store)).not.toBeNull();
  });
});

describe('overlay wire', () => {
  test('reads every shape the Go engine writes', () => {
    const golden = readFileSync(join(import.meta.dir, '../../../../../internal/domain/automation/testdata/overlay_wire.golden.json'), 'utf8');
    const items = (JSON.parse(golden) as unknown[]).map((entry) => readOverlayMessage(JSON.stringify(entry)));
    expect(items).toEqual([
      { kind: 'text', content: '</script><img src=x onerror=alert(1)>', seconds: 5, position: 'top' },
      { kind: 'image', content: 'https://cdn.example.com/a.png', seconds: 60, position: 'center' },
      { kind: 'sound', content: 'https://cdn.example.com/horn.mp3', volume: 0 }
    ]);
  });

  test('media urls need https, a host and no credentials', () => {
    expect(overlayMediaUrl('https://cdn.example.com/a.png')).toBe(true);
    for (const bad of ['http://cdn.example.com/a.png', 'javascript:alert(1)', 'data:image/png;base64,AA', 'https://u:p@x.example/', '/a.png', '']) {
      expect(overlayMediaUrl(bad)).toBe(false);
    }
  });

  test('unsafe media and junk never become frames', () => {
    for (const kind of ['image', 'video', 'sound']) {
      expect(readOverlayMessage(JSON.stringify({ v: 1, kind, content: 'javascript:alert(1)', seconds: 5 }))).toBeNull();
      expect(readOverlayMessage(JSON.stringify({ v: 1, kind, content: 'http://x.example/a', seconds: 5 }))).toBeNull();
    }
    for (const junk of ['', 'nope', 'null', '[]', '{}', '{"v":2,"kind":"text","content":"x"}', '{"v":1,"kind":"html","content":"https://x.example"}', '{"v":1,"kind":"text"}', `{"v":1,"kind":"text","content":"${'a'.repeat(9000)}"}`]) {
      expect(readOverlayMessage(junk)).toBeNull();
    }
  });

  test('a frame stays one SSE event whatever the content holds, and drops unknown fields', () => {
    const item = readOverlayMessage(JSON.stringify({ v: 1, kind: 'text', content: 'a\n\nevent: evil\ndata: x\r\n', seconds: 500, position: 'side', extra: 'x' }))!;
    expect(item).toEqual({ kind: 'text', content: 'a\n\nevent: evil\ndata: x\r\n', seconds: 60, position: 'center' });
    const frame = overlayItemFrame(item as Exclude<typeof item, 'revoke'>);
    expect(frame.match(/\n/g)).toHaveLength(3);
    expect(frame).not.toContain('\r');
  });
});

describe('overlay hub', () => {
  test('one channel never reaches another channel\'s streams', async () => {
    const { bus, hub } = hubOn();
    const one = open(hub, '1');
    const two = open(hub, '2');
    await readFrames(one.reader, 2);
    await readFrames(two.reader, 2);
    bus.publish('overlay:1', TEXT);
    bus.publish('overlay:2', JSON.stringify({ v: 1, kind: 'text', content: 'for two', seconds: 5, position: 'top' }));
    expect((await readFrames(one.reader, 1))[0]).toContain('"content":"hi"');
    expect((await readFrames(two.reader, 1))[0]).toContain('for two');
    one.abort.abort();
    two.abort.abort();
  });

  test('frames arrive in publish order', async () => {
    const { bus, hub } = hubOn();
    const client = open(hub);
    await readFrames(client.reader, 2);
    for (let i = 0; i < 5; i++) {
      bus.publish('overlay:1', JSON.stringify({ v: 1, kind: 'text', content: `n${i}`, seconds: 1, position: 'top' }));
    }
    const frames = await readFrames(client.reader, 5);
    expect(frames.map((f) => /"content":"(n\d)"/.exec(f)?.[1])).toEqual(['n0', 'n1', 'n2', 'n3', 'n4']);
    client.abort.abort();
  });

  test('a revoke ends every stream of that channel only', async () => {
    const { bus, hub } = hubOn();
    const a = open(hub, '1');
    const b = open(hub, '1');
    const other = open(hub, '2');
    await Promise.all([readFrames(a.reader, 2), readFrames(b.reader, 2), readFrames(other.reader, 2)]);
    bus.publish('overlay:1', OVERLAY_REVOKE_MESSAGE);
    expect((await a.reader.read()).done).toBe(true);
    expect((await b.reader.read()).done).toBe(true);
    expect(hub.size()).toEqual({ channels: 1, streams: 1, connected: true });
    expect(bus.subscribed.has('overlay:1')).toBe(false);
    other.abort.abort();
  });

  test('a recheck ends a stream whose token no longer matches, keeps it on a failed read', async () => {
    let answer: () => Promise<boolean> = () => Promise.reject(new Error('rpc down'));
    const { hub } = hubOn(fakeBus(), () => answer());
    const client = open(hub);
    await readFrames(client.reader, 2);
    await hub.recheck();
    expect(hub.size().streams).toBe(1);
    answer = () => Promise.resolve(false);
    await hub.recheck();
    expect((await client.reader.read()).done).toBe(true);
    expect(hub.size().streams).toBe(0);
  });

  test('aborting the request releases the stream, the subscription and the connection', async () => {
    const { bus, hub } = hubOn();
    const client = open(hub);
    expect(hub.size()).toEqual({ channels: 1, streams: 1, connected: true });
    expect(bus.subscribed.has('overlay:1')).toBe(true);
    client.abort.abort();
    expect(hub.size()).toEqual({ channels: 0, streams: 0, connected: false });
    expect(bus.subscribed.size).toBe(0);
    expect(bus.state.closes).toBe(1);
  });

  test('cancelling the body releases the stream too', async () => {
    const { hub } = hubOn();
    const client = open(hub);
    await client.reader.cancel();
    expect(hub.size().streams).toBe(0);
  });

  test('a client that stops reading is dropped at the buffer bound, never queued past it', async () => {
    const { bus, hub } = hubOn();
    const client = open(hub);
    let frames = 0;
    for (let i = 0; i < 1000; i++) {
      bus.publish('overlay:1', TEXT);
      if (hub.size().streams === 0) break;
      frames += 1;
    }
    expect(frames).toBeLessThanOrEqual(16);
    expect(hub.size()).toEqual({ channels: 0, streams: 0, connected: false });
    await expect(client.reader.read()).rejects.toThrow();
  });

  test('the oldest stream gives way past the per-channel cap', async () => {
    const { hub } = hubOn();
    const clients = Array.from({ length: 9 }, () => open(hub));
    expect(hub.size().streams).toBe(8);
    await readFrames(clients[0]!.reader, 2);
    expect((await clients[0]!.reader.read()).done).toBe(true);
    for (const client of clients) client.abort.abort();
    expect(hub.size().streams).toBe(0);
  });

  test('a reconnect subscribes again and a stuck connection is rebuilt', async () => {
    const { bus, hub } = hubOn();
    const client = open(hub);
    bus.subscribed.clear();
    bus.state.events?.onReady();
    expect(bus.subscribed.has('overlay:1')).toBe(true);
    bus.state.isReady = false;
    await hub.recheck();
    expect(bus.state.connects).toBe(1);
    await hub.recheck();
    expect(bus.state.connects).toBe(2);
    expect(bus.state.closes).toBe(1);
    client.abort.abort();
  });

  test('pings stop after abort', () => {
    jest.useFakeTimers();
    try {
      const { hub } = hubOn();
      const client = open(hub);
      const before = jest.getTimerCount();
      client.abort.abort();
      expect(jest.getTimerCount()).toBeLessThan(before);
      expect(jest.getTimerCount()).toBe(0);
    } finally {
      jest.useRealTimers();
    }
  });
});

describe('overlay page', () => {
  test('sends the strict policy, no cookies and no caching', async () => {
    const res = overlayPageResponse();
    const csp = res.headers.get('content-security-policy')!;
    expect(csp).toBe(OVERLAY_CSP);
    expect(csp).toContain("default-src 'none'");
    expect(csp).toContain('img-src https:;');
    expect(csp).toContain('media-src https:;');
    expect(csp).toContain("connect-src 'self'");
    expect(csp).not.toContain('unsafe');
    expect(res.headers.has('set-cookie')).toBe(false);
    expect(res.headers.get('cache-control')).toBe('no-store');
    expect(res.headers.get('referrer-policy')).toBe('no-referrer');
    expect(res.headers.get('x-robots-tag')).toContain('noindex');
    const html = await res.text();
    expect(html).toContain('noindex');
    expect(html).toContain('data-cfasync="false"');
  });

  test('the policy hashes exactly the shipped script and style', async () => {
    const { createHash } = await import('node:crypto');
    const html = await overlayPageResponse().text();
    const script = /<script[^>]*>([\s\S]*?)<\/script>/.exec(html)![1]!;
    const style = /<style>([\s\S]*?)<\/style>/.exec(html)![1]!;
    const hash = (s: string) => createHash('sha256').update(s).digest('base64');
    expect(OVERLAY_CSP).toContain(`script-src 'sha256-${hash(script)}'`);
    expect(OVERLAY_CSP).toContain(`style-src 'sha256-${hash(style)}'`);
    expect(html.match(/<script/g)).toHaveLength(1);
  });

  test('a wrong token answers a bare 404', async () => {
    const res = overlayNotFound();
    expect(res.status).toBe(404);
    expect(await res.text()).toBe('Not Found');
    expect(res.headers.has('set-cookie')).toBe(false);
  });

  test('the client uses no HTML sinks', () => {
    for (const sink of ['innerHTML', 'outerHTML', 'insertAdjacent', 'document.write', 'eval(', 'Function(', 'srcdoc', 'localStorage', 'document.cookie']) {
      expect(OVERLAY_SCRIPT).not.toContain(sink);
    }
  });
});

interface FakeNode {
  tag: string;
  className: string;
  src?: string;
  volume?: number;
  textContent?: string;
  listeners: Record<string, () => void>;
  addEventListener(type: string, fn: () => void): void;
  remove(): void;
  removeAttribute(name: string): void;
  pause?: () => void;
  play?: () => Promise<void>;
}

function runClient() {
  const nodes: FakeNode[] = [];
  const onStage = new Set<FakeNode>();
  const sources: { url: string; listeners: Record<string, (e: { data: string }) => void>; onerror?: () => void; closed: boolean }[] = [];
  const document = {
    body: { appendChild: (node: FakeNode) => onStage.add(node) },
    createElement(tag: string): FakeNode {
      const node: FakeNode = {
        tag,
        className: '',
        listeners: {},
        addEventListener(type, fn) {
          this.listeners[type] = fn;
        },
        remove() {
          onStage.delete(this);
        },
        removeAttribute(name) {
          if (name === 'src') delete this.src;
        }
      };
      if (tag === 'video' || tag === 'audio') {
        node.pause = () => {};
        node.play = () => Promise.resolve();
      }
      nodes.push(node);
      return node;
    }
  };
  class FakeEventSource {
    url: string;
    listeners: Record<string, (e: { data: string }) => void> = {};
    onerror?: () => void;
    closed = false;
    constructor(url: string) {
      this.url = url;
      sources.push(this);
    }
    addEventListener(type: string, fn: (e: { data: string }) => void) {
      this.listeners[type] = fn;
    }
    close() {
      this.closed = true;
    }
  }
  new Function('document', 'location', 'EventSource', OVERLAY_SCRIPT)(document, { pathname: '/overlay/1.secret' }, FakeEventSource);
  const send = (item: unknown) => sources.at(-1)!.listeners.item!({ data: JSON.stringify(item) });
  return { nodes, onStage, sources, send };
}

describe('overlay client', () => {
  beforeEach(() => jest.useFakeTimers());
  afterEach(() => jest.useRealTimers());

  test('connects to its own stream path', () => {
    expect(runClient().sources[0]!.url).toBe('/overlay/1.secret/stream');
  });

  test('markup in content lands as literal text', () => {
    const { nodes, send } = runClient();
    for (const content of ['</script><b>x</b>', '<img src=x onerror=alert(1)>']) {
      send({ kind: 'text', content, seconds: 1, position: 'top' });
      jest.advanceTimersByTime(1000);
    }
    expect(nodes.map((n) => [n.tag, n.textContent])).toEqual([
      ['div', '</script><b>x</b>'],
      ['div', '<img src=x onerror=alert(1)>']
    ]);
  });

  test('refuses non-https media before creating an element', () => {
    const { nodes, send } = runClient();
    for (const kind of ['image', 'video', 'sound']) {
      for (const content of ['http://x.example/a', 'javascript:alert(1)', 'data:image/png;base64,AA', '/relative']) {
        send({ kind, content, seconds: 1, position: 'top', volume: 50 });
      }
    }
    expect(nodes).toHaveLength(0);
    send({ kind: 'image', content: 'https://x.example/a.png', seconds: 1, position: 'top' });
    expect(nodes.map((n) => n.src)).toEqual(['https://x.example/a.png']);
  });

  test('plays one item at a time, in order, for its seconds', () => {
    const { nodes, onStage, send } = runClient();
    send({ kind: 'text', content: 'first', seconds: 2, position: 'top' });
    send({ kind: 'text', content: 'second', seconds: 1, position: 'bottom' });
    expect(nodes.map((n) => n.textContent)).toEqual(['first']);
    jest.advanceTimersByTime(1999);
    expect(nodes).toHaveLength(1);
    jest.advanceTimersByTime(1);
    expect(nodes.map((n) => n.textContent)).toEqual(['first', 'second']);
    expect(onStage.size).toBe(1);
    expect(nodes[1]!.className).toContain('bottom');
    jest.advanceTimersByTime(1000);
    expect(onStage.size).toBe(0);
  });

  test('a broken image gives way to the next item', () => {
    const { nodes, send } = runClient();
    send({ kind: 'image', content: 'https://x.example/broken.png', seconds: 60, position: 'center' });
    send({ kind: 'text', content: 'next', seconds: 1, position: 'center' });
    nodes[0]!.listeners.error!();
    expect(nodes[1]!.textContent).toBe('next');
  });

  test('sound sets its volume and waits for the end', () => {
    const { nodes, send } = runClient();
    send({ kind: 'sound', content: 'https://x.example/a.mp3', volume: 40 });
    send({ kind: 'text', content: 'after', seconds: 1, position: 'center' });
    expect(nodes[0]!.volume).toBe(0.4);
    expect(nodes).toHaveLength(1);
    nodes[0]!.listeners.ended!();
    expect(nodes[1]!.textContent).toBe('after');
  });

  test('the local queue is bounded', () => {
    const { nodes, send } = runClient();
    for (let i = 0; i < 100; i++) send({ kind: 'text', content: String(i), seconds: 1, position: 'top' });
    jest.advanceTimersByTime(200_000);
    expect(nodes).toHaveLength(33);
  });

  test('a lost stream closes and reconnects with backoff', () => {
    const { sources } = runClient();
    sources[0]!.onerror!();
    expect(sources[0]!.closed).toBe(true);
    expect(sources).toHaveLength(1);
    jest.advanceTimersByTime(2000);
    expect(sources).toHaveLength(2);
    sources[1]!.onerror!();
    jest.advanceTimersByTime(2000);
    expect(sources).toHaveLength(2);
    jest.advanceTimersByTime(2000);
    expect(sources).toHaveLength(3);
  });
});
