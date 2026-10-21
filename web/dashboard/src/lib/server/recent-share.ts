// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

export function shareRecent<T>(ttlMs: number): (key: string, make: () => T) => T {
  const recent = new Map<string, T>();
  return (key, make) => {
    if (recent.has(key)) return recent.get(key) as T;
    const value = make();
    recent.set(key, value);
    setTimeout(() => {
      if (recent.get(key) === value) recent.delete(key);
    }, ttlMs).unref?.();
    return value;
  };
}
