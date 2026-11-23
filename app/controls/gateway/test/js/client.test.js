// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
'use strict';

const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');

const CLIENT_SOURCE = fs.readFileSync(path.join(__dirname, '../../priv/overlay/client.js'), 'utf8');
const START_MS = Date.parse('2026-11-23T12:00:00Z');
const MINUTE_MS = 60000;
const BASE = '/tts';

const flush = async () => {
  for (let i = 0; i < 8; i++) await new Promise((resolve) => setImmediate(resolve));
};

const reply = (status, retryAfter) => ({
  status,
  headers: { get: (name) => (name === 'retry-after' && retryAfter !== undefined ? String(retryAfter) : null) }
});

class FakeEventSource {
  static CLOSED = 2;
  static instances = [];

  constructor(url) {
    this.url = url;
    this.readyState = 1;
    this.closed = false;
    this.listeners = {};
    FakeEventSource.instances.push(this);
  }

  addEventListener(name, listener) {
    this.listeners[name] = listener;
  }

  close() {
    this.closed = true;
    this.readyState = FakeEventSource.CLOSED;
  }

  emit(name, data) {
    this.listeners[name]({ data: typeof data === 'string' ? data : JSON.stringify(data) });
  }

  fail() {
    this.readyState = FakeEventSource.CLOSED;
    this.onerror();
  }
}

function fakeTimers(clock) {
  const pending = [];
  const delays = [];
  let nextId = 1;
  return {
    delays,
    set(fn, ms) {
      delays.push(ms);
      pending.push({ id: nextId, at: clock.now + ms, fn });
      return nextId++;
    },
    clear(id) {
      const index = pending.findIndex((timer) => timer.id === id);
      if (index >= 0) pending.splice(index, 1);
    },
    fireLast() {
      const timer = pending.pop();
      timer.fn();
    },
    advance(ms) {
      const target = clock.now + ms;
      for (;;) {
        pending.sort((a, b) => a.at - b.at);
        if (pending.length === 0 || pending[0].at > target) break;
        const timer = pending.shift();
        clock.now = Math.max(clock.now, timer.at);
        timer.fn();
      }
      clock.now = target;
    }
  };
}

function harness({ token = '', random = () => 0.5, stored = null, store = new Map(), respond = () => null, hold = false, failWrites = false } = {}) {
  FakeEventSource.instances = [];
  const clock = { now: START_MS };
  const timers = fakeTimers(clock);
  if (stored) store.set('bb_seen:' + BASE, JSON.stringify(stored));
  const h = { clock, timers, store, requests: [], alerts: [], stops: [], revisions: [], feeds: [], ctls: [] };
  const fetch = (url, init) => {
    const body = init.body ? JSON.parse(init.body) : null;
    h.requests.push({ url, method: init.method, body });
    const custom = respond(url, init.method);
    const fallback = url.endsWith('/events') ? reply(200) : reply(204);
    return Promise.resolve(custom || fallback);
  };
  const context = vm.createContext({});
  vm.runInContext(CLIENT_SOURCE, context);
  h.client = context.createOverlayClient({
    EventSource: FakeEventSource,
    fetch,
    storage: {
      getItem: (key) => store.get(key) ?? null,
      setItem: (key, value) => {
        if (failWrites) throw new Error('quota');
        store.set(key, value);
      }
    },
    now: () => clock.now,
    setTimeout: (fn, ms) => timers.set(fn, ms),
    clearTimeout: (id) => timers.clear(id),
    random,
    basePath: BASE,
    token,
    onAlert: (alert, ctl) => {
      h.alerts.push(alert.event_id);
      h.ctls.push(ctl);
      if (hold) return;
      ctl.started();
      ctl.done();
    },
    onRevision: (n) => h.revisions.push(n),
    onFeed: (name, data) => h.feeds.push([name, JSON.parse(data)]),
    onStop: (reason) => h.stops.push(reason)
  });
  h.sources = FakeEventSource.instances;
  h.acks = () => h.requests.filter((r) => r.url === BASE + '/ack').map((r) => [r.body.handle, r.body.reason]);
  h.sessions = () => h.requests.filter((r) => r.url === BASE + '/session').length;
  return h;
}

async function open(options) {
  const h = harness(options);
  h.client.start();
  await flush();
  return h;
}

const frame = (id, offsetMs = 5 * MINUTE_MS) => ({
  event_id: id,
  kind: 'tts',
  handle: 'h-' + id,
  expires_at: new Date(START_MS + offsetMs).toISOString(),
  data: { clip_key: 'k-' + id }
});

const alertFrames = (h, frames) => frames.forEach((f) => h.sources[0].emit('alert', f));

test('alert frames are deduplicated, dropped on reload and dropped when expired', async () => {
  const live = START_MS + 5 * MINUTE_MS;
  const cases = [
    { name: 'duplicate frame', stored: null, frames: [frame('a'), frame('a')], played: ['a'],
      acks: [['h-a', 'played'], ['h-a', 'duplicate']] },
    { name: 'reload drops redelivered ids', stored: { a: live }, frames: [frame('a'), frame('b')], played: ['b'],
      acks: [['h-a', 'duplicate'], ['h-b', 'played']] },
    { name: 'past expires_at', stored: null, frames: [frame('a', -1000), frame('b')], played: ['b'],
      acks: [['h-a', 'expired'], ['h-b', 'played']] }
  ];
  for (const c of cases) {
    const h = await open({ stored: c.stored });
    alertFrames(h, c.frames);
    await flush();
    assert.deepEqual(
      { name: c.name, played: h.alerts, acks: h.acks() },
      { name: c.name, played: c.played, acks: c.acks }
    );
  }
});

test('a redelivered id still queued is acked duplicate and not queued twice', async () => {
  const h = await open({ hold: true });
  alertFrames(h, [frame('a'), frame('b'), frame('b')]);
  h.ctls[0].started();
  h.ctls[0].done();
  await flush();
  assert.deepEqual(
    { played: h.alerts, acks: h.acks() },
    { played: ['a', 'b'], acks: [['h-b', 'duplicate'], ['h-a', 'played']] }
  );
});

test('started takes an explicit reason and expired reports the server clock', async () => {
  const h = await open({ hold: true });
  alertFrames(h, [frame('a', MINUTE_MS), frame('b')]);
  const before = h.ctls[0].expired();
  h.clock.now += 2 * MINUTE_MS;
  const after = h.ctls[0].expired();
  h.ctls[0].started('expired');
  h.ctls[0].done();
  h.ctls[1].started('dropped');
  assert.deepEqual(
    { before, after, acks: h.acks() },
    { before: false, after: true, acks: [['h-a', 'expired'], ['h-b', 'dropped']] }
  );
});

test('started acks played and done only advances the queue', async () => {
  const h = await open({ hold: true });
  alertFrames(h, [frame('a'), frame('b')]);
  h.ctls[0].done();
  const afterDone = { played: h.alerts.slice(), acks: h.acks() };
  h.ctls[1].started();
  h.ctls[1].started();
  assert.deepEqual(
    { afterDone, acks: h.acks() },
    { afterDone: { played: ['a', 'b'], acks: [] }, acks: [['h-b', 'played']] }
  );
});

test('an item expiring while queued is acked expired when it reaches the head', async () => {
  const h = await open({ hold: true });
  alertFrames(h, [frame('a'), frame('b', MINUTE_MS), frame('c')]);
  h.clock.now += 2 * MINUTE_MS;
  h.ctls[0].started();
  h.ctls[0].done();
  await flush();
  assert.deepEqual(
    { played: h.alerts, acks: h.acks() },
    { played: ['a', 'c'], acks: [['h-a', 'played'], ['h-b', 'expired']] }
  );
});

test('expiry uses the server clock from ready', async () => {
  const cases = [
    { name: 'same clock', skewMs: 0, played: ['a'], acks: [['h-a', 'played']] },
    { name: 'server ahead', skewMs: 10 * MINUTE_MS, played: [], acks: [['h-a', 'expired']] },
    { name: 'server behind', skewMs: -10 * MINUTE_MS, played: ['a'], acks: [['h-a', 'played']] }
  ];
  for (const c of cases) {
    const h = await open();
    h.sources[0].emit('ready', { now: START_MS + c.skewMs, key: 'k' });
    alertFrames(h, [frame('a')]);
    await flush();
    assert.deepEqual(
      { name: c.name, played: h.alerts, acks: h.acks() },
      { name: c.name, played: c.played, acks: c.acks }
    );
  }
});

test('seen is keyed by overlay, merged across clients and pruned of expired entries', async () => {
  const stored = { old: START_MS - 1, fresh: START_MS + MINUTE_MS };
  const store = new Map([['bb_seen:' + BASE, JSON.stringify(stored)]]);
  const first = await open({ store });
  const afterLoad = JSON.parse(store.get('bb_seen:' + BASE));
  first.sources[0].emit('ready', { now: START_MS, key: 'k' });
  const second = await open({ store });
  second.sources[0].emit('ready', { now: START_MS, key: 'k' });
  first.sources[0].emit('alert', frame('a'));
  second.sources[0].emit('alert', frame('b'));
  second.sources[0].emit('alert', frame('a'));
  first.sources[0].emit('alert', frame('c'));
  await flush();
  assert.deepEqual(
    { afterLoad, keys: Object.keys(JSON.parse(store.get('bb_seen:k'))).sort(), first: first.alerts, second: second.alerts },
    { afterLoad: { fresh: START_MS + MINUTE_MS }, keys: ['a', 'b', 'c', 'fresh'], first: ['a', 'c'], second: ['b'] }
  );
});

test('feed frames reach onFeed with their name and data', async () => {
  const h = await open();
  const frames = { snapshot: { stats: {} }, stats: { counts: {} }, subathon: { running: true }, control: { paused: false } };
  Object.entries(frames).forEach(([name, data]) => h.sources[0].emit(name, data));
  assert.deepEqual(h.feeds, Object.entries(frames));
});

test('ready reaches onRevision only when it carries a revision', async () => {
  const cases = [
    { ready: { now: START_MS, key: 'k', revision: 7 }, revisions: [7] },
    { ready: { now: START_MS, key: 'k' }, revisions: [] }
  ];
  for (const c of cases) {
    const h = await open();
    h.sources[0].emit('ready', c.ready);
    assert.deepEqual(h.revisions, c.revisions);
  }
});

test('the events check opens v2 directly on 200 and exchanges the token on 401', async () => {
  const cases = [
    { name: 'head 200', head: 200, sessions: 0 },
    { name: 'head 401', head: 401, sessions: 1 }
  ];
  for (const c of cases) {
    const respond = (url, method) => (method === 'HEAD' ? reply(c.head) : null);
    const h = await open({ token: 'tok', respond });
    assert.deepEqual(
      { name: c.name, sessions: h.sessions(), urls: h.sources.map((s) => s.url) },
      { name: c.name, sessions: c.sessions, urls: [BASE + '/events'] }
    );
  }
});

test('handoff closes the old stream only after the new one is ready', async () => {
  const cases = [
    { name: 'new ready', finish: (h) => h.sources[1].emit('ready', {}), oldClosed: true, sources: 2 },
    { name: 'new not ready yet', finish: () => {}, oldClosed: false, sources: 2 },
    { name: 'old errors first', finish: (h) => h.sources[0].fail(), oldClosed: true, sources: 2 }
  ];
  for (const c of cases) {
    const h = await open();
    h.sources[0].emit('ready', {});
    h.sources[0].emit('reconnect', { after_ms: 300 });
    h.timers.advance(299);
    const beforeDelay = h.sources.length;
    h.timers.advance(1);
    c.finish(h);
    await flush();
    assert.deepEqual(
      { name: c.name, beforeDelay, oldClosed: h.sources[0].closed, sources: h.sources.length },
      { name: c.name, beforeDelay: 1, oldClosed: c.oldClosed, sources: c.sources }
    );
  }
});

test('an old stream failing before the new one is ready hands over to the pending stream', async () => {
  const cases = [
    { name: 'pending ready', next: (h) => h.sources[1].emit('ready', {}) },
    { name: 'pending alert first', next: (h) => h.sources[1].emit('alert', frame('a')) }
  ];
  for (const c of cases) {
    const h = await open();
    h.sources[0].emit('reconnect', { after_ms: 0 });
    h.timers.advance(0);
    h.sources[0].fail();
    c.next(h);
    h.sources[1].emit('alert', frame('b'));
    await flush();
    assert.deepEqual(
      {
        name: c.name,
        sources: h.sources.length,
        oldClosed: h.sources[0].closed,
        newClosed: h.sources[1].closed,
        heads: h.requests.filter((r) => r.method === 'HEAD').length,
        timers: h.timers.delays.length
      },
      { name: c.name, sources: 2, oldClosed: true, newClosed: false, heads: 1, timers: 2 }
    );
  }
});

test('a pending stream failing after the old one is gone reconnects normally', async () => {
  const h = await open();
  h.sources[0].emit('reconnect', { after_ms: 0 });
  h.timers.advance(0);
  h.sources[0].fail();
  h.sources[1].fail();
  h.timers.fireLast();
  await flush();
  h.sources[2].emit('alert', frame('a'));
  assert.deepEqual(
    { sources: h.sources.length, heads: h.requests.filter((r) => r.method === 'HEAD').length, played: h.alerts },
    { sources: 3, heads: 2, played: ['a'] }
  );
});

test('a replacement failing before ready is retried while the old stream lives', async () => {
  const h = await open();
  h.sources[0].emit('reconnect', { after_ms: 0 });
  h.timers.advance(0);
  h.sources[1].fail();
  h.timers.fireLast();
  await flush();
  h.sources[2].emit('ready', {});
  assert.deepEqual(
    { sources: h.sources.length, oldClosed: h.sources[0].closed },
    { sources: 3, oldClosed: true }
  );
});

test('a replacement retry checks the stream first and honors retry-after', async () => {
  let head = 200;
  const h = await open({ random: () => 0, respond: (url, method) => (method === 'HEAD' ? reply(head, 7) : null) });
  h.sources[0].emit('reconnect', { after_ms: 0 });
  h.timers.advance(0);
  head = 503;
  h.sources[1].fail();
  h.timers.fireLast();
  await flush();
  const waited = h.timers.delays[h.timers.delays.length - 1];
  head = 200;
  h.timers.fireLast();
  await flush();
  assert.deepEqual(
    { waited, sources: h.sources.length, oldClosed: h.sources[0].closed },
    { waited: 7000, sources: 3, oldClosed: false }
  );
});

test('a server fin that leaves the stream connecting reconnects through the events check', async () => {
  const h = await open();
  h.sources[0].emit('ready', {});
  h.sources[0].readyState = 0;
  h.sources[0].onerror();
  const closed = h.sources[0].closed;
  h.timers.advance(60000);
  await flush();
  assert.deepEqual(
    { closed, sources: h.sources.length, heads: h.requests.filter((r) => r.method === 'HEAD').length },
    { closed: true, sources: 2, heads: 2 }
  );
});

test('seen survives ready when storage writes fail', async () => {
  const cases = [
    { name: 'same key', keys: ['k', 'k'] },
    { name: 'key changes', keys: ['k', 'k2'] }
  ];
  for (const c of cases) {
    const h = await open({ failWrites: true });
    h.sources[0].emit('ready', { key: c.keys[0] });
    h.sources[0].emit('alert', frame('a'));
    h.sources[0].emit('ready', { key: c.keys[1] });
    h.sources[0].emit('alert', frame('a'));
    await flush();
    assert.deepEqual(
      { name: c.name, played: h.alerts, acks: h.acks() },
      { name: c.name, played: ['a'], acks: [['h-a', 'played'], ['h-a', 'duplicate']] }
    );
  }
});

test('reconnect delays are bounded by the doubling cap and jittered', async () => {
  const cases = [
    { random: () => 1, expected: [1000, 2000, 4000, 8000, 15000, 15000] },
    { random: () => 0, expected: [500, 500, 500, 500, 500, 500] }
  ];
  for (const c of cases) {
    let head = 200;
    const h = await open({ random: c.random, respond: (url, method) => (method === 'HEAD' ? reply(head) : null) });
    head = 500;
    h.sources[0].fail();
    const delays = [];
    for (let i = 0; i < c.expected.length; i++) {
      delays.push(h.timers.delays[h.timers.delays.length - 1]);
      h.timers.fireLast();
      await flush();
    }
    assert.deepEqual(delays, c.expected);
  }
});

test('the first frame of any kind resets the backoff', async () => {
  const h = await open({ random: () => 1 });
  const delays = [];
  for (let i = 0; i < 3; i++) {
    h.sources[h.sources.length - 1].fail();
    delays.push(h.timers.delays[h.timers.delays.length - 1]);
    h.timers.fireLast();
    await flush();
    if (i === 1) h.sources[h.sources.length - 1].emit('revision', 4);
  }
  assert.deepEqual({ delays, revisions: h.revisions }, { delays: [1000, 2000, 1000], revisions: [4] });
});

test('503 and 429 wait at least retry-after', async () => {
  const cases = [
    { name: 'events check', token: '', head: reply(503, 7), session: null, expected: 7000 },
    { name: 'session exchange', token: 'tok', head: reply(401), session: reply(429, 9), expected: 9000 }
  ];
  for (const c of cases) {
    const respond = (url, method) => (method === 'HEAD' ? c.head : url.endsWith('/session') ? c.session : null);
    const h = await open({ token: c.token, random: () => 0, respond });
    assert.equal(h.timers.delays[h.timers.delays.length - 1], c.expected, c.name);
  }
});

test('204 from the events check and 404 from the session stop for good', async () => {
  const cases = [
    { name: 'head 204', token: '', head: reply(204), session: null },
    { name: 'session 404', token: 'tok', head: reply(401), session: reply(404) }
  ];
  for (const c of cases) {
    const respond = (url, method) => (method === 'HEAD' ? c.head : url.endsWith('/session') ? c.session : null);
    const h = await open({ token: c.token, respond });
    assert.deepEqual(
      { name: c.name, stops: h.stops, sources: h.sources.length },
      { name: c.name, stops: ['gone'], sources: 0 }
    );
  }
});

test('acks retry on 5xx three times 1 s apart and stop once settled', async () => {
  const cases = [
    { name: 'recovers', statuses: [503, 503, 204], requests: 3 },
    { name: 'settled by 401', statuses: [401], requests: 1 },
    { name: 'gives up', statuses: [500, 500, 500, 500], requests: 3 }
  ];
  for (const c of cases) {
    let call = 0;
    const respond = (url) => (url.endsWith('/ack') ? reply(c.statuses[call++] ?? 204) : null);
    const h = await open({ respond });
    h.sources[0].emit('alert', frame('a'));
    for (let i = 0; i < 4; i++) {
      await flush();
      h.timers.advance(1000);
    }
    await flush();
    assert.deepEqual({ name: c.name, requests: h.acks().length }, { name: c.name, requests: c.requests });
  }
});

test('progress is posted for queued handles in batches of at most 32', async () => {
  const h = await open({ hold: true });
  for (let i = 0; i < 70; i++) h.sources[0].emit('alert', frame('e' + i));
  h.timers.advance(15000);
  const sizes = h.requests.filter((r) => r.url === BASE + '/progress').map((r) => r.body.handles.length);
  assert.deepEqual(sizes, [32, 32, 6]);
});
