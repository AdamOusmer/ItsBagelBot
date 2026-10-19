// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

function familyOf(key: string): string | null {
  const first = key.indexOf(':');
  return first < 0 ? null : key.slice(0, first + 1);
}

function bucketOf(key: string): string | null {
  const first = key.indexOf(':');
  if (first < 0) return null;
  const second = key.indexOf(':', first + 1);
  return second < 0 ? key : key.slice(0, second);
}

function addTo(index: Map<string, Set<string>>, group: string | null, key: string): void {
  if (group === null) return;
  const set = index.get(group);
  if (set) set.add(key);
  else index.set(group, new Set([key]));
}

function removeFrom(index: Map<string, Set<string>>, group: string | null, key: string): void {
  if (group === null) return;
  const set = index.get(group);
  if (!set) return;
  set.delete(key);
  if (set.size === 0) index.delete(group);
}

function isBucketPrefix(prefix: string): boolean {
  const first = prefix.indexOf(':');
  return first >= 0 && first < prefix.length - 1 && prefix.indexOf(':', first + 1) < 0;
}

// `fam:` matches the whole family, `fam:id` matches `fam:id` and `fam:id:*` but not `fam:idX`.
export function matchesPrefix(prefix: string, key: string): boolean {
  return isBucketPrefix(prefix) ? bucketOf(key) === prefix : key.startsWith(prefix);
}

// Groups keys as `family:` and `family:id` so a prefix invalidation touches only its own keys.
export class PrefixIndex {
  private readonly families = new Map<string, Set<string>>();
  private readonly buckets = new Map<string, Set<string>>();

  add(key: string): void {
    addTo(this.families, familyOf(key), key);
    addTo(this.buckets, bucketOf(key), key);
  }

  remove(key: string): void {
    removeFrom(this.families, familyOf(key), key);
    removeFrom(this.buckets, bucketOf(key), key);
  }

  clear(): void {
    this.families.clear();
    this.buckets.clear();
  }

  // Same matches as matchesPrefix; null means the prefix has no colon and needs a full scan.
  matching(prefix: string): string[] | null {
    const first = prefix.indexOf(':');
    if (first < 0) return null;
    if (first === prefix.length - 1) return [...(this.families.get(prefix) ?? [])];
    const bucket = bucketOf(prefix) as string;
    const keys = [...(this.buckets.get(bucket) ?? [])];
    return bucket === prefix ? keys : keys.filter((key) => key.startsWith(prefix));
  }
}
