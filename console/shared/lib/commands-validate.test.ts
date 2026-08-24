// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { describe, expect, test } from 'bun:test';
import {
  DEFS_PER_BROADCASTER,
  FETCH_NAME_MAX,
  FETCH_URL_MAX,
  JSON_PATH_MAX_DEPTH,
  KEY_LABEL_MAX,
  KEY_VALUE_MAX,
  URLFETCH_TOKEN_CAP,
  buildJsonPath,
  malformedUrlFetchTokens,
  normalizeCommandResponse,
  parseJsonPath,
  slugifyName,
  urlFetchNames,
  validateCommand,
  validateFetchDef
} from './commands-validate';
import { RateLimiter } from './server/rate-limit';

const validFields = {
  name: 'raid',
  aliases: [],
  cooldown: 0,
  allowedUserId: ''
};

describe('command response normalization', () => {
  test('preserves message boundaries as LF separators', () => {
    expect(normalizeCommandResponse('first message\r\nsecond message')).toBe('first message\nsecond message');
    expect(normalizeCommandResponse('first message\rsecond message')).toBe('first message\nsecond message');
  });

  test('drops blank lines and trailing whitespace without joining messages', () => {
    expect(normalizeCommandResponse('first  \n\nsecond\t\n')).toBe('first\nsecond');
  });

  test('rejects non-newline control characters', () => {
    expect(validateCommand({ ...validFields, response: 'first\u0000second' }).response).toBe(
      'Response cannot contain control characters.'
    );
  });

  test('caps distinct {urlfetch} references per response', () => {
    const n = URLFETCH_TOKEN_CAP;
    const many = Array.from({ length: n + 1 }, (_, i) => `{urlfetch:def_${i}}`).join(' ');
    expect(validateCommand({ ...validFields, response: many }).response).toContain('at most');
    // Repeats of the same def collapse: occurrences above the cap are fine
    // when they resolve to fewer distinct names (the engine dedupes too).
    const repeats = Array.from({ length: n + 3 }, () => '{urlfetch:def_0}').join(' ');
    expect(validateCommand({ ...validFields, response: repeats }).response).toBeUndefined();
    expect(validateCommand({ ...validFields, response: `{urlfetch:a} ok` }).response).toBeUndefined();
  });
});

// --- urlfetch definitions ---------------------------------------------------

const validDef = {
  name: 'weather',
  url: 'https://api.example.com/v1/wx?q=london',
  kind: 'json' as const,
  path: ['forecast', 'current', 'temp_f'],
  keyLabel: ''
};

describe('fetch-def validation parity table', () => {
  test('name grammar ^[a-z0-9_]{1,32}$', () => {
    expect(validateFetchDef({ ...validDef, name: '' }).name).toBeDefined();
    expect(validateFetchDef({ ...validDef, name: 'a'.repeat(FETCH_NAME_MAX) }).name).toBeUndefined();
    expect(validateFetchDef({ ...validDef, name: 'a'.repeat(FETCH_NAME_MAX + 1) }).name).toContain('32');
    expect(validateFetchDef({ ...validDef, name: 'Weather' }).name).toBeDefined();
    expect(validateFetchDef({ ...validDef, name: 'has space' }).name).toBeDefined();
    // The def-name grammar has no hyphen: the stored name charset is exactly
    // [a-z0-9_], matching Go's FetchDefName sentinel table.
    expect(validateFetchDef({ ...validDef, name: 'ok_name1' }).name).toBeUndefined();
    expect(validateFetchDef({ ...validDef, name: 'bad-name' }).name).toBeDefined();
  });

  test('url https-only, <=512, public host shape', () => {
    expect(validateFetchDef({ ...validDef, url: 'http://api.example.com/x' }).url).toContain('https');
    expect(validateFetchDef({ ...validDef, url: 'ftp://api.example.com/x' }).url).toBeDefined();
    expect(validateFetchDef({ ...validDef, url: 'a'.repeat(FETCH_URL_MAX + 1) }).url).toContain('512');
    expect(validateFetchDef({ ...validDef, url: `https://a.co/${'x'.repeat(FETCH_URL_MAX - 13)}` }).url).toBeUndefined();
    for (const host of ['127.0.0.1', '169.254.169.254', '[::1]', 'localhost', 'printer.local', 'postgres.internal']) {
      expect(validateFetchDef({ ...validDef, url: `https://${host}/x` }).url).toBeDefined();
    }
    expect(validateFetchDef({ ...validDef, url: 'not a url' }).url).toBeDefined();
  });

  test('path depth <=8, segment grammar, plain takes no path', () => {
    expect(validateFetchDef({ ...validDef, path: [] }).path).toBeUndefined();
    expect(
      validateFetchDef({ ...validDef, path: Array.from({ length: JSON_PATH_MAX_DEPTH }, (_, i) => `s${i}`) }).path
    ).toBeUndefined();
    expect(
      validateFetchDef({ ...validDef, path: Array.from({ length: JSON_PATH_MAX_DEPTH + 1 }, (_, i) => `s${i}`) }).path
    ).toContain('8');
    expect(validateFetchDef({ ...validDef, path: ['a', 'b c'] }).path).toBeDefined();
    expect(validateFetchDef({ ...validDef, path: ['items', '0'] }).path).toBeUndefined(); // array index as bare digits
    expect(validateFetchDef({ ...validDef, path: [], kind: 'plain' }).path).toBeUndefined();
    expect(validateFetchDef({ ...validDef, path: ['x'], kind: 'plain' }).path).toBeDefined();
  });

  test('key label / value ceilings', () => {
    expect(validateFetchDef({ ...validDef, keyLabel: 'k'.repeat(KEY_LABEL_MAX) }).key_label).toBeUndefined();
    expect(validateFetchDef({ ...validDef, keyLabel: 'k'.repeat(KEY_LABEL_MAX + 1) }).key_label).toContain('32');
    expect(KEY_VALUE_MAX).toBe(512);
    expect(DEFS_PER_BROADCASTER).toBe(20);
  });
});

describe('slugifyName', () => {
  test('mirrors normName discipline then folds to the grammar', () => {
    expect(slugifyName('  Weather Now! ')).toBe('weather_now');
    // Contiguous non-grammar runes fold to ONE underscore; edge underscores trim.
    expect(slugifyName('ÀÉÎ -- cool??')).toBe('cool');
    expect(slugifyName('!!!')).toBe('');
  });

  test('trims underscore edges produced by folding and slicing', () => {
    expect(slugifyName('__temp__')).toBe('temp');
    expect(slugifyName(` ${'a'.repeat(50)} `)).toBe('a'.repeat(FETCH_NAME_MAX));
    expect(slugifyName(`${'a'.repeat(31)} b`)).toBe('a'.repeat(31));
    expect(slugifyName('!!!')).toBe('');
  });

  test('output always satisfies the name grammar when non-empty', () => {
    for (const raw of ['Hello, World', '  x  ', 'ABC_def-9', '???', 'a\tb\nc']) {
      const slug = slugifyName(raw);
      if (slug !== '') expect(slug).toMatch(/^[a-z0-9_]+$/);
    }
  });
});

describe('urlfetch token scanning', () => {
  test('first-appearance order, deduped, fast-path bail', () => {
    expect(urlFetchNames('no tokens here')).toEqual([]);
    expect(urlFetchNames('{urlfetch:b} {urlfetch:a} {urlfetch:b}')).toEqual(['b', 'a']);
    expect(urlFetchNames('{urlfetch:WEATHER.CURRENT}')).toEqual(['weather.current']); // payload folded
    expect(urlFetchNames('{urlfetch broken')).toEqual([]);
    expect(urlFetchNames('{user} typed {urlfetch:w.x} and {choice:a,b}')).toEqual(['w.x']);
  });

  test('malformed spans are named verbatim', () => {
    expect(malformedUrlFetchTokens('{urlfetch:ok.def} fine')).toEqual([]);
    expect(malformedUrlFetchTokens('{urlfetch} {urlfetch:} {urlfetch:a..b} {urlfetch unclosed')).toEqual([
      '{urlfetch}',
      '{urlfetch:}',
      '{urlfetch:a..b}',
      '{urlfetch unclosed'
    ]);
  });

  test('dotted path parse/build round-trip', () => {
    expect(buildJsonPath(['forecast', 'current', 'temp_f'])).toBe('forecast.current.temp_f');
    expect(parseJsonPath('forecast.current.temp_f')).toEqual(['forecast', 'current', 'temp_f']);
    expect(parseJsonPath('')).toEqual([]);
    expect(parseJsonPath('a..b')).toBeNull();
    expect(parseJsonPath('a b.c')).toBeNull();
  });
});

// --- path-builder property vs a local resolver mirror ------------------------
//
// The Go gossip resolver reads `$.seg.seg[0].seg` shapes over decoded JSON;
// this mirror is the smallest honest stand-in: walk segments over an object,
// bare-digit segments index arrays. The property under test: for every leaf
// the picker can produce from a fixture, buildJsonPath -> parseJsonPath ->
// mirror-resolve lands on the same leaf value. If the builder ever emits a
// spelling the grammar (or the resolver) disagrees with, this catches it.

interface Leaf {
  path: string[];
  value: unknown;
}

function collectLeaves(node: unknown, prefix: string[], out: Leaf[]): void {
  if (Array.isArray(node)) {
    node.forEach((child, i) => collectLeaves(child, [...prefix, String(i)], out));
    return;
  }
  if (node !== null && typeof node === 'object') {
    for (const [k, v] of Object.entries(node)) collectLeaves(v, [...prefix, k], out);
    return;
  }
  out.push({ path: prefix, value: node });
}

function resolveMirror(root: unknown, segments: string[]): unknown {
  let cur: unknown = root;
  for (const seg of segments) {
    if (cur === null || typeof cur !== 'object') return undefined;
    cur = (cur as Record<string, unknown>)[seg];
  }
  return cur;
}

describe('picker path-building property', () => {
  const fixture = {
    forecast: { current: { temp_f: 71.2, condition: { text: 'Sunny' } }, daily: [{ max: 80 }, { max: 75 }] },
    location: { name: 'London', lat: 51.5 },
    scalar_root: 'top-level'
  };

  test('every picker leaf round-trips through the token grammar to the same value', () => {
    const leaves: Leaf[] = [];
    collectLeaves(fixture, [], leaves);
    expect(leaves.length).toBeGreaterThan(6);

    for (const leaf of leaves) {
      const dotted = buildJsonPath(leaf.path);
      const parsed = parseJsonPath(dotted);
      expect(parsed).not.toBeNull();
      expect(parsed).toEqual(leaf.path); // builder -> parser identity
      expect(resolveMirror(fixture, parsed!)).toEqual(leaf.value); // parser -> resolver identity
      if (leaf.path.length > 0) {
        expect(dotted.length).toBeGreaterThan(0);
        expect(malformedUrlFetchTokens(`{urlfetch:name.${dotted}}`)).toEqual([]); // grammar-clean token
      }
    }
  });

  test('depth cap excludes exactly the too-deep picks', () => {
    const deep = { a: { b: { c: { d: { e: { f: { g: { h: { i: 1 } } } } } } } } };
    const leaves: Leaf[] = [];
    collectLeaves(deep, [], leaves);
    const tooDeep = leaves.filter((l) => l.path.length > JSON_PATH_MAX_DEPTH);
    const pickable = leaves.filter((l) => l.path.length <= JSON_PATH_MAX_DEPTH);
    expect(pickable.every((l) => validateFetchDef({ ...validDef, path: l.path }).path === undefined)).toBe(true);
    expect(tooDeep.every((l) => validateFetchDef({ ...validDef, path: l.path }).path !== undefined)).toBe(true);
  });
});

// --- rehearsal limiter numbers -----------------------------------------------
//
// The fetches page wires ValkeyRateLimiter({ capacity: 6, refillPerSec: 0.1 })
// keyed fetchtest:<uid> — one dry-run per 10s sustained, burst 6 — because
// each run dials a third-party API. The Valkey limiter degrades to this exact
// in-memory bucket semantics, so the numbers are pinned here against that
// class: the 7th immediate call must reject with a ~10s retry horizon.

describe('fetchtest limiter numbers (capacity 6, refill 0.1/s)', () => {
  let t = 0;
  const limiter = new RateLimiter({ capacity: 6, refillPerSec: 0.1, now: () => t });
  const key = 'fetchtest:42';

  test('burst of 6 passes instantly, 7th rejects', () => {
    t = 1000;
    for (let i = 0; i < 6; i++) expect(limiter.check(key).allowed).toBe(true);
    const rejected = limiter.check(key);
    expect(rejected.allowed).toBe(false);
    expect(rejected.retryAfterSec).toBe(10); // one token at 0.1/s
  });

  test('sustained rate is one run per 10 seconds', () => {
    t += 9999; // just short of a full refill
    expect(limiter.check(key).allowed).toBe(false);
    t += 1; // 10s elapsed since the burst drained: exactly one token back
    expect(limiter.check(key).allowed).toBe(true);
    expect(limiter.check(key).allowed).toBe(false);
    limiter.dispose();
  });
});
