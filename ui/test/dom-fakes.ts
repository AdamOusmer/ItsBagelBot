// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { copyFileSync, mkdtempSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

export type Listener = (event: unknown) => void;

export function eventTarget() {
  const listeners = new Map<string, Set<Listener>>();
  return {
    addEventListener(type: string, fn: Listener) {
      if (!listeners.has(type)) listeners.set(type, new Set());
      listeners.get(type)?.add(fn);
    },
    removeEventListener(type: string, fn: Listener) {
      listeners.get(type)?.delete(fn);
    },
    dispatch(type: string, event: unknown = {}) {
      for (const fn of listeners.get(type) ?? []) fn(event);
    },
    count(type: string) {
      return listeners.get(type)?.size ?? 0;
    },
  };
}

export function snapshotGlobals(keys: string[]): () => void {
  const saved = keys.map((key) => [key, Object.getOwnPropertyDescriptor(globalThis, key)] as const);
  return () => {
    for (const [key, descriptor] of saved) {
      if (descriptor) Object.defineProperty(globalThis, key, descriptor);
      else Reflect.deleteProperty(globalThis, key);
    }
  };
}

export function isolatedLib(prefix: string, files: string[]) {
  const dir = mkdtempSync(join(tmpdir(), `bagel-${prefix}-test-`));
  for (const file of files) copyFileSync(new URL(`../lib/${file}`, import.meta.url), join(dir, file));
  return {
    load: <T>(file: string) => import(join(dir, file)) as Promise<T>,
    remove: () => rmSync(dir, { recursive: true, force: true }),
  };
}
