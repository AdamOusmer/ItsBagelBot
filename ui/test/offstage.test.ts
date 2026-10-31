// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { afterAll, beforeEach, describe, expect, test } from "bun:test";
import { snapshotGlobals } from './dom-fakes';
import { watchOffstage } from '../lib/offstage';

const restoreGlobals = snapshotGlobals(['IntersectionObserver']);

type Callback = (entries: IntersectionObserverEntry[]) => void;

const documentTimeline = { kind: 'document' };
const scrollTimeline = { kind: 'scroll' };

let callback: Callback = () => {};
let disconnected = false;

globalThis.IntersectionObserver = class {
  constructor(cb: Callback) {
    callback = cb;
  }
  observe() {}
  disconnect() {
    disconnected = true;
  }
} as unknown as typeof IntersectionObserver;

class FakeAnimation {
  plays = 0;
  pauses = 0;
  constructor(
    readonly iterations: number,
    readonly timeline: unknown = documentTimeline,
    public playState = 'running',
  ) {}
  get effect() {
    return { getTiming: () => ({ iterations: this.iterations }) };
  }
  pause() {
    this.pauses++;
    this.playState = 'paused';
  }
  play() {
    this.plays++;
    this.playState = 'running';
  }
}

function section(animations: FakeAnimation[]) {
  return {
    ownerDocument: { timeline: documentTimeline },
    getAnimations: () => animations,
  };
}

function watch(targets: unknown[]) {
  const root = { querySelectorAll: () => targets } as unknown as ParentNode;
  return watchOffstage(root, 'section');
}

function emit(target: unknown, isIntersecting: boolean) {
  callback([{ target, isIntersecting } as unknown as IntersectionObserverEntry]);
}

beforeEach(() => {
  disconnected = false;
});

afterAll(restoreGlobals);

describe('watchOffstage', () => {
  test('pauses an infinite animation on leave and plays it on return', () => {
    const loop = new FakeAnimation(Infinity);
    const target = section([loop]);
    watch([target]);

    emit(target, false);
    expect(loop.pauses).toBe(1);
    expect(loop.playState).toBe('paused');

    emit(target, true);
    expect(loop.plays).toBe(1);
    expect(loop.playState).toBe('running');
  });

  test('leaves finite animations alone', () => {
    const once = new FakeAnimation(1);
    const target = section([once]);
    watch([target]);

    emit(target, false);
    emit(target, true);

    expect(once.pauses).toBe(0);
    expect(once.plays).toBe(0);
  });

  test('leaves scroll-driven animations alone', () => {
    const driven = new FakeAnimation(Infinity, scrollTimeline);
    const target = section([driven]);
    watch([target]);

    emit(target, false);
    emit(target, true);

    expect(driven.pauses).toBe(0);
    expect(driven.plays).toBe(0);
  });

  test('does not play an animation the page paused itself', () => {
    const external = new FakeAnimation(Infinity, documentTimeline, 'paused');
    const target = section([external]);
    watch([target]);

    emit(target, false);
    emit(target, true);

    expect(external.pauses).toBe(0);
    expect(external.plays).toBe(0);
  });

  test('dispose plays what it paused and disconnects', () => {
    const loop = new FakeAnimation(Infinity);
    const target = section([loop]);
    const dispose = watch([target]);

    emit(target, false);
    dispose();

    expect(loop.plays).toBe(1);
    expect(disconnected).toBe(true);
  });

  test('does nothing without getAnimations', () => {
    const dispose = watch([{ ownerDocument: { timeline: documentTimeline } }]);
    expect(() => dispose()).not.toThrow();
  });
});
