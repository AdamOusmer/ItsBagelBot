// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
// @ts-ignore Bun supplies this module at test runtime.
import { describe, expect, test } from 'bun:test';
import { ReadOrder } from '../src/lib/components/shards/read-order';

describe('independent shard reads', () => {
  test('a dispatched slow poll does not suppress the initial fleet response', () => {
    const order = new ReadOrder();
    const poll = order.start();
    expect(order.accept(0)).toBe(true);
    expect(order.accept(poll)).toBe(true);
  });
  test('an unavailable poll leaves the first usable initial response eligible', () => {
    const order = new ReadOrder();
    expect(order.current(order.start())).toBe(true);
    expect(order.accept(0)).toBe(true);
  });
  test('a successful live response rejects the older initial response', () => {
    const order = new ReadOrder();
    expect(order.accept(order.start())).toBe(true);
    expect(order.accept(0)).toBe(false);
  });
  test('out-of-order completed polls cannot overwrite fresh state or record stale failures', () => {
    const order = new ReadOrder();
    const slow = order.start();
    const fresh = order.start();
    expect(order.accept(fresh)).toBe(true);
    expect(order.accept(slow)).toBe(false);
    expect(order.current(slow)).toBe(false);
  });
  test('slow trial reads cannot hold back or overwrite fleet reads', () => {
    const fleet = new ReadOrder();
    const trials = new ReadOrder();
    trials.start();
    expect(fleet.accept(fleet.start())).toBe(true);
    expect(trials.accept(0)).toBe(true);
    expect(fleet.accept(0)).toBe(false);
  });
  test('a mutation fences outstanding polls and leaves subsequent reads eligible', () => {
    const order = new ReadOrder();
    const beforeSave = order.start();
    order.invalidate();
    expect(order.accept(beforeSave)).toBe(false);
    expect(order.accept(0)).toBe(false);
    const duringSave = order.start();
    order.invalidate();
    expect(order.accept(duringSave)).toBe(false);
    expect(order.current(duringSave)).toBe(false);
    expect(order.accept(order.start())).toBe(true);
  });

});
