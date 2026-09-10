// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// The curated opening set, pinned. See the golden-output-tests skill: the
// heads are written out here as a literal and the LENGTH is asserted, so
// widening the offer to six is a deliberate edit to two files rather than
// something that happens because somebody appended to a catalog. The whole
// value of the list is that a rehearsal surface shows five chips and nothing
// else -- no "More variables" button, no expand-in-place, no hidden list.
//
// The two catalogs it curates cannot both be imported here -- @bagel/kit must
// not depend on the marketing site -- so the fixtures below are the two real
// spellings copied verbatim from
// web/dashboard/src/lib/components/commands/ResponseEditor.svelte and
// web/marketing/src/i18n/builder.ts, INCLUDING the pairs that differ
// (`{count:deaths}` vs `{count:falls}`) and the near-duplicates that made
// de-duping necessary (`{random}` beside `{random:1-6}`). If a catalog renames
// one of those, this test still passes and it should: the head is what the
// list is keyed on, which is the point.

import { describe, expect, test } from 'bun:test';
import {
  COMMON_TOKEN_HEADS,
  COMMON_TOKEN_LIMIT,
  isCommonToken,
  pickCommonTokens,
  tokenHead,
} from './common-tokens';

test('the opening set is exactly these five heads, in this order', () => {
  expect([...COMMON_TOKEN_HEADS]).toEqual(['user', 'args', 'channel', 'random', 'uptime']);
  expect(COMMON_TOKEN_HEADS.length).toBeLessThanOrEqual(5);
  expect(COMMON_TOKEN_LIMIT).toBe(5);
});

describe('tokenHead', () => {
  test('splits a payload off at the colon', () => {
    expect(tokenHead('{count:deaths}')).toBe('count');
    expect(tokenHead('{count:falls}')).toBe('count');
    expect(tokenHead('{random:1-6}')).toBe('random');
    expect(tokenHead('{if:touser:hi there:hi everyone}')).toBe('if');
  });

  // The dot is not a split point, and this is the test that says why: these
  // are five DIFFERENT variables, and a head that stopped at the dot would
  // pull all five into a list whose whole job is to be five long.
  test('keeps a dotted name whole', () => {
    expect(tokenHead('{user.login}')).toBe('user.login');
    expect(tokenHead('{random.chatter}')).toBe('random.chatter');
    expect(tokenHead('{random.emote}')).toBe('random.emote');
    expect(tokenHead('{channel.viewers}')).toBe('channel.viewers');
    expect(tokenHead('{user}')).toBe('user');
  });

  test('anything that is not one whole token has no head', () => {
    expect(tokenHead('hello {user}')).toBe('');
    expect(tokenHead('{user} {channel}')).toBe('');
    expect(tokenHead('user')).toBe('');
    expect(tokenHead('')).toBe('');
  });
});

test('isCommonToken keys on the head, not the example payload', () => {
  expect(isCommonToken('{uptime}')).toBe(true);
  // `{count:deaths}` is an example payload, not a variable: cut 2026-09-10.
  expect(isCommonToken('{count:deaths}')).toBe(false);
  expect(isCommonToken('{count:falls}')).toBe(false);
  // The console swaps {counter:…} for the counter PICKER, so the bumping
  // counter is deliberately NOT in the opening set: it would show the same
  // variable twice there.
  expect(isCommonToken('{counter:deaths}')).toBe(false);
  expect(isCommonToken('{user.login}')).toBe(false);
  expect(isCommonToken('{followage}')).toBe(false);
  // Cut from the set when it went from eight to five: real, documented, and
  // reachable somewhere other than the rehearsal surface.
  expect(isCommonToken('{game}')).toBe(false);
  expect(isCommonToken('{title}')).toBe(false);
});

/** The console palette, in ResponseEditor's own order, counter chip removed. */
const CONSOLE_CATALOG = [
  '{user}', '{target}', '{args}', '{1}', '{2:}', '{channel}', '{userid}',
  '{user.login}', '{command}', '{random}', '{choice:a,b,c}', '{math:1+1}',
  '{querystring}', '{queryescape:text}', '{pathescape:text}', '{repeat:3:hi}',
  '{countdown:2026-12-25}', '{countup:2026-12-25}',
  '{if:touser:hi there:hi everyone}', '{followage}', '{accountage}', '{points}',
  '{pointsname}', '{watchtime}', '{count:deaths}', '{uses}', '{quote}',
  '{time}', '{song}', '{chatters}', '{random.chatter}', '{7tvemotes}',
  '{random.emote}', '{uptime}', '{title}', '{game}', '{channel.viewers}',
];

/** The marketing builder's custom-command catalog, in its own order. */
const MARKETING_CATALOG = [
  '{user}', '{touser}', '{args}', '{1}', '{2:}', '{channel}', '{userid}',
  '{user.login}', '{command}', '{counter:falls}', '{count:falls}', '{uses}',
  '{random}', '{random:1-6}', '{choice:yes,no,maybe}', '{math:1+2*3}',
  '{querystring}', '{queryescape:hello world}', '{pathescape:hello world}',
  '{repeat:3:bagel}', '{countdown:2026-12-25}', '{countup:2020-01-01}',
  '{chatters}', '{random.chatter}', '{7tvemotes}', '{bttvemotes}',
  '{ffzemotes}', '{random.emote}', '{uptime}', '{title}', '{game}',
  '{channel.viewers}', '{followage}', '{accountage}', '{points}',
  '{pointsname}', '{watchtime}', '{quote}', '{time}', '{song}',
];

describe('pickCommonTokens', () => {
  test('the console opens on five, in the console catalog order', () => {
    const picked = pickCommonTokens(CONSOLE_CATALOG, (t) => t);
    expect(picked).toEqual([
      '{user}', '{args}', '{channel}', '{random}', '{uptime}',
    ]);
    expect(picked.length).toBeLessThanOrEqual(5);
  });

  test('the marketing builder opens on the same five variables', () => {
    const picked = pickCommonTokens(MARKETING_CATALOG, (t) => t);
    expect(picked).toEqual([
      '{user}', '{args}', '{channel}', '{random}', '{uptime}',
    ]);
    expect(picked.length).toBeLessThanOrEqual(5);
  });

  test('a counter example never takes a slot, even when the catalog has one', () => {
    expect(pickCommonTokens(['{count:deaths}', '{uptime}'], (t) => t)).toEqual(['{uptime}']);
  });

  // The reason this is a function rather than a filter each surface writes.
  test('a second example of the same variable does not take a second slot', () => {
    expect(pickCommonTokens(['{random}', '{random:1-6}'], (t) => t)).toEqual(['{random}']);
    expect(pickCommonTokens(['{random:1-6}', '{random}'], (t) => t)).toEqual(['{random:1-6}']);
  });

  test('a short catalog with none of the five comes back empty', () => {
    expect(pickCommonTokens(['{followage}', '{points}'], (t) => t)).toEqual([]);
  });
});
