// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
var createOverlayClient = (function () {
  const ACK_ATTEMPTS = 3;
  const ACK_GAP_MS = 1000;
  const ACK_SETTLED = [200, 204, 401, 404];
  const PROGRESS_MS = 15000;
  const PROGRESS_BATCH = 32;
  const MIN_RETRY_MS = 500;
  const FIRST_RETRY_MS = 1000;
  const MAX_RETRY_MS = 15000;
  const JSON_HEADERS = { 'Content-Type': 'application/json' };

  function isOk(status) {
    return status >= 200 && status < 300;
  }

  function retryAfterMs(response, now) {
    const raw = response && response.headers.get('retry-after');
    if (!raw) return 0;
    const seconds = Number(raw);
    const delay = Number.isFinite(seconds) ? seconds * 1000 : Date.parse(raw) - now;
    return Math.max(0, delay || 0);
  }

  class SeenStore {
    constructor(storage, key, now, carried) {
      this.storage = storage;
      this.key = key;
      this.now = now;
      this.entries = Object.assign({}, carried);
      this.merge();
    }

    merge() {
      Object.assign(this.entries, this.load());
      this.prune();
    }

    load() {
      try {
        const parsed = JSON.parse(this.storage.getItem(this.key) || '{}');
        return parsed !== null && typeof parsed === 'object' ? parsed : {};
      } catch (_) {
        return {};
      }
    }

    has(id) {
      this.merge();
      return Object.prototype.hasOwnProperty.call(this.entries, id);
    }

    add(id, expiresMs) {
      this.merge();
      this.entries[id] = expiresMs;
      this.prune();
      this.save();
    }

    prune() {
      const now = this.now();
      const stale = Object.keys(this.entries).filter((id) => !(this.entries[id] > now));
      stale.forEach((id) => delete this.entries[id]);
      if (stale.length > 0) this.save();
    }

    save() {
      try {
        this.storage.setItem(this.key, JSON.stringify(this.entries));
      } catch (_) {}
    }
  }

  class Backoff {
    constructor(random) {
      this.random = random;
      this.step = 0;
    }

    next(floorMs) {
      const cap = Math.min(MAX_RETRY_MS, FIRST_RETRY_MS * Math.pow(2, this.step));
      this.step += 1;
      return Math.max(floorMs, MIN_RETRY_MS, this.random() * cap);
    }

    reset() {
      this.step = 0;
    }
  }

  class Http {
    constructor(env) {
      this.env = env;
    }

    send(path, init) {
      const options = Object.assign({ credentials: 'same-origin', redirect: 'error', cache: 'no-store' }, init);
      return this.env.fetch(this.env.basePath + path, options).catch(() => null);
    }

    post(path, body) {
      return this.send(path, { method: 'POST', headers: JSON_HEADERS, body: JSON.stringify(body) });
    }

    async ack(handle, reason) {
      for (let attempt = 0; attempt < ACK_ATTEMPTS; attempt++) {
        const response = await this.post('/ack', { handle: handle, reason: reason });
        if (response && ACK_SETTLED.includes(response.status)) return;
        await new Promise((resolve) => this.env.setTimeout(resolve, ACK_GAP_MS));
      }
    }

    progress(handles) {
      for (let start = 0; start < handles.length; start += PROGRESS_BATCH) {
        this.post('/progress', { handles: handles.slice(start, start + PROGRESS_BATCH) });
      }
    }
  }

  class OverlayClient {
    constructor(env) {
      this.env = env;
      this.http = new Http(env);
      this.skewMs = 0;
      this.seen = this.seenFor(env.basePath, {});
      this.backoff = new Backoff(env.random);
      this.queue = [];
      this.busy = false;
      this.running = false;
      this.generation = 0;
      this.floorMs = 0;
      this.source = null;
      this.pending = null;
      this.reconnectTimer = null;
      this.handoffTimer = null;
      this.progressTimer = null;
    }

    seenFor(key, carried) {
      return new SeenStore(this.env.storage, 'bb_seen:' + key, () => this.serverNow(), carried);
    }

    serverNow() {
      return this.env.now() + this.skewMs;
    }

    start() {
      if (this.running) return;
      this.running = true;
      this.scheduleProgress();
      this.reconnect();
    }

    stop() {
      this.running = false;
      this.generation += 1;
      this.dropStreams();
      [this.reconnectTimer, this.progressTimer].forEach((timer) => this.env.clearTimeout(timer));
      this.reconnectTimer = null;
      this.progressTimer = null;
      this.queue = [];
      this.busy = false;
    }

    finish(reason) {
      this.stop();
      this.env.onStop(reason);
    }

    dropStreams() {
      this.generation += 1;
      [this.source, this.pending].forEach((source) => source && source.close());
      this.source = null;
      this.pending = null;
      this.env.clearTimeout(this.handoffTimer);
      this.handoffTimer = null;
    }

    async exchange() {
      const response = await this.http.post('/session', { token: this.env.token });
      const status = response ? response.status : 0;
      if (isOk(status)) return 'ok';
      if (status === 404) return 'gone';
      this.floorMs = retryAfterMs(response, this.env.now());
      return 'retry';
    }

    async exchangeThenOpen(handoff) {
      const generation = this.generation;
      const state = await this.exchange();
      if (generation !== this.generation) return;
      if (state === 'gone') return this.finish('gone');
      if (state === 'retry') return this.retryLater(handoff);
      this.attach(handoff);
    }

    attach(handoff) {
      if (!handoff) {
        this.source = this.open();
        return;
      }
      this.handoffTimer = null;
      this.pending = this.open();
    }

    async reconnect(handoff) {
      const generation = this.generation;
      const response = await this.http.send('/events', { method: 'HEAD' });
      if (generation !== this.generation) return;
      const status = response ? response.status : 0;
      if (status === 204) return this.finish('gone');
      if (isOk(status)) return this.attach(handoff);
      if (status === 401 && this.env.token) return this.exchangeThenOpen(handoff);
      this.floorMs = retryAfterMs(response, this.env.now());
      this.retryLater(handoff);
    }

    retryLater(handoff) {
      const delay = this.backoff.next(this.floorMs);
      this.floorMs = 0;
      const timer = this.env.setTimeout(() => this.reconnect(handoff), delay);
      if (handoff) this.handoffTimer = timer;
      else this.reconnectTimer = timer;
    }

    open() {
      const source = new this.env.EventSource(this.env.basePath + '/events');
      const handlers = {
        alert: (message) => this.receive(JSON.parse(message.data)),
        ready: (message) => this.ready(source, message),
        revision: (message) => this.env.onRevision(Number(message.data)),
        reconnect: (message) => this.beginHandoff(message)
      };
      Object.keys(handlers).forEach((name) => {
        source.addEventListener(name, (message) => {
          if (source !== this.source && source !== this.pending) return;
          this.backoff.reset();
          handlers[name](message);
        });
      });
      source.onerror = () => this.failed(source);
      return source;
    }

    failed(source) {
      if (source === this.pending) {
        source.close();
        this.pending = null;
        this.retryLater(this.source !== null);
        return;
      }
      if (source !== this.source) return;
      if (this.pending) return this.adoptPending();
      this.dropStreams();
      this.retryLater();
    }

    adoptPending() {
      this.source.close();
      this.source = null;
    }

    beginHandoff(message) {
      if (this.pending || this.handoffTimer !== null) return;
      this.scheduleHandoff(Number(JSON.parse(message.data).after_ms) || 0);
    }

    scheduleHandoff(delayMs) {
      this.handoffTimer = this.env.setTimeout(() => {
        this.handoffTimer = null;
        this.pending = this.open();
      }, delayMs);
    }

    ready(source, message) {
      this.promote(source);
      const info = JSON.parse(message.data);
      if (typeof info.now === 'number') this.skewMs = info.now - this.env.now();
      if (typeof info.key === 'string' && this.seen.key !== 'bb_seen:' + info.key) {
        this.seen = this.seenFor(info.key, this.seen.entries);
      }
      if (typeof info.revision === 'number') this.env.onRevision(info.revision);
    }

    promote(source) {
      if (source !== this.pending) return;
      const previous = this.source;
      this.source = source;
      this.pending = null;
      if (previous) previous.close();
    }

    receive(alert) {
      if (typeof alert.event_id !== 'string' || typeof alert.handle !== 'string') return;
      const parsed = Date.parse(alert.expires_at);
      const expiresMs = Number.isNaN(parsed) ? this.serverNow() + MAX_RETRY_MS : parsed;
      const reason = this.rejection(alert.event_id, expiresMs);
      if (reason) {
        this.http.ack(alert.handle, reason);
        return;
      }
      this.seen.add(alert.event_id, expiresMs);
      this.queue.push({ alert: alert, expiresMs: expiresMs, started: false, finished: false });
      this.pump();
    }

    rejection(id, expiresMs) {
      if (this.seen.has(id)) return 'duplicate';
      return expiresMs <= this.serverNow() ? 'expired' : null;
    }

    pump() {
      if (this.busy || this.queue.length === 0) return;
      const item = this.queue[0];
      if (item.expiresMs <= this.serverNow()) {
        this.queue.shift();
        this.http.ack(item.alert.handle, 'expired');
        this.pump();
        return;
      }
      this.busy = true;
      this.env.onAlert(item.alert, {
        started: (reason) => this.markStarted(item, reason || 'played'),
        done: () => this.complete(item),
        expired: () => item.expiresMs <= this.serverNow()
      });
    }

    markStarted(item, reason) {
      if (item.started) return;
      item.started = true;
      if (item.alert.handle) this.http.ack(item.alert.handle, reason);
    }

    complete(item) {
      if (item.finished) return;
      item.finished = true;
      if (this.queue[0] !== item) return;
      this.queue.shift();
      this.busy = false;
      this.pump();
    }

    scheduleProgress() {
      this.progressTimer = this.env.setTimeout(() => {
        this.http.progress(this.queue.filter((item) => !item.started && item.alert.handle).map((item) => item.alert.handle));
        this.scheduleProgress();
      }, PROGRESS_MS);
    }
  }

  return function createOverlayClient(env) {
    return new OverlayClient(env);
  };
})();
