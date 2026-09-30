// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

export type CachedMeasure<T> = {
  read(): T;
  watch(): void;
  unwatch(): void;
};

export function cachedMeasure<T>(measure: () => T): CachedMeasure<T> {
  let value: T | undefined;
  let fresh = false;
  const stale = (): void => {
    fresh = false;
  };
  return {
    read() {
      if (!fresh) {
        value = measure();
        fresh = true;
      }
      return value as T;
    },
    watch() {
      fresh = false;
      window.addEventListener('scroll', stale, { passive: true, capture: true });
      window.addEventListener('resize', stale, { passive: true });
    },
    unwatch() {
      fresh = false;
      window.removeEventListener('scroll', stale, true);
      window.removeEventListener('resize', stale);
    },
  };
}
