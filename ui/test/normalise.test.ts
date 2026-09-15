// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
import { expect, test } from 'bun:test';
import { normalise } from './normalise';

test('hydration markers and the bare Svelte 5 anchor disappear', () => {
  expect(normalise('<!--[--><p class="bb-fixture">hello</p><!--]-->')).toBe(
    '<p class="bb-fixture">hello</p>',
  );
  expect(normalise('<select><!>opt</select>')).toBe('<select>opt</select>');
});

test('a comment that itself contains <!-- is removed, as are adjacent comments', () => {
  expect(normalise('<!--<!-- -->done')).toBe('done');
  expect(normalise('<!--><!-- --><em>x</em>')).toBe('<em>x</em>');
  expect(normalise('<!--a--><!--b-->z')).toBe('z');
});

test('framework bookkeeping still collapses to the contract', () => {
  expect(normalise('<img src="x" />')).toBe('<img src="x">');
  expect(normalise('<p data-fixture="">x</p>')).toBe('<p data-fixture>x</p>');
  expect(
    normalise('<p>a</p>\n<script type="module" src="/engine.js"></script>\n<p>b</p>'),
  ).toBe('<p>a</p><p>b</p>');
});
