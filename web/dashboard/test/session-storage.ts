// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { afterAll, beforeEach } from 'bun:test';

export const storage = new Map<string, string>();

const fake = {
  get length() {
    return storage.size;
  },
  key: (index: number) => [...storage.keys()][index] ?? null,
  getItem: (key: string) => storage.get(key) ?? null,
  setItem: (key: string, value: string) => void storage.set(key, value),
  removeItem: (key: string) => void storage.delete(key)
};

export function useFakeSessionStorage(): void {
  const original = Object.getOwnPropertyDescriptor(globalThis, 'sessionStorage');
  Object.defineProperty(globalThis, 'sessionStorage', { configurable: true, value: fake });
  beforeEach(() => storage.clear());
  afterAll(() => {
    if (original) Object.defineProperty(globalThis, 'sessionStorage', original);
    else Reflect.deleteProperty(globalThis, 'sessionStorage');
  });
}

export function withUnavailableSessionStorage<T>(run: () => T): T {
  Object.defineProperty(globalThis, 'sessionStorage', {
    configurable: true,
    get() {
      throw new Error('storage unavailable');
    }
  });
  try {
    return run();
  } finally {
    Object.defineProperty(globalThis, 'sessionStorage', { configurable: true, value: fake });
  }
}
