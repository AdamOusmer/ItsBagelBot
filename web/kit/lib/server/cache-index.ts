// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// `fam:` names a family, `fam:id` a bucket; a key belongs to both groups.
interface ParsedKey {
  text: string;
  family: string | null;
  bucket: string | null;
}

type Groups = Map<string, Set<string>>;

function parse(text: string): ParsedKey {
  const first = text.indexOf(':');
  if (first < 0) return { text, family: null, bucket: null };
  const second = text.indexOf(':', first + 1);
  return { text, family: text.slice(0, first + 1), bucket: second < 0 ? text : text.slice(0, second) };
}

function groupNames(key: ParsedKey): string[] {
  return key.family === null || key.bucket === null ? [] : [key.family, key.bucket];
}

function link(groups: Groups, key: ParsedKey): void {
  for (const name of groupNames(key)) {
    const set = groups.get(name);
    if (set) set.add(key.text);
    else groups.set(name, new Set([key.text]));
  }
}

function unlink(groups: Groups, key: ParsedKey): void {
  for (const name of groupNames(key)) {
    const set = groups.get(name);
    set?.delete(key.text);
    if (set?.size === 0) groups.delete(name);
  }
}

// `fam:` matches the whole family, `fam:id` matches `fam:id` and `fam:id:*` but not `fam:idX`.
function covers(prefix: ParsedKey, key: ParsedKey): boolean {
  if (prefix.text === prefix.family) return key.family === prefix.family;
  if (prefix.text === prefix.bucket) return key.bucket === prefix.bucket;
  return key.text.startsWith(prefix.text);
}

export function matchesAny(prefixes: readonly string[], key: string): boolean {
  const parsed = parse(key);
  return prefixes.some((prefix) => covers(parse(prefix), parsed));
}

export class PrefixIndex {
  private readonly groups: Groups = new Map();

  add(key: string): void {
    link(this.groups, parse(key));
  }

  remove(key: string): void {
    unlink(this.groups, parse(key));
  }

  clear(): void {
    this.groups.clear();
  }

  // A prefix without a colon has no group, so only it falls back to scanning every key.
  matching(prefixes: readonly string[], all: () => Iterable<string>): Set<string> {
    const found = new Set<string>();
    for (const prefix of prefixes.map(parse)) {
      const group = prefix.text === prefix.family ? prefix.family : prefix.bucket;
      const candidates = group === null ? all() : (this.groups.get(group) ?? []);
      const exact = group === prefix.text;
      for (const key of candidates) if (exact || covers(prefix, parse(key))) found.add(key);
    }
    return found;
  }
}
