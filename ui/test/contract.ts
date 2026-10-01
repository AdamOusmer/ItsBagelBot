// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { expect, test } from 'bun:test';
import { createRawSnippet } from 'svelte';
import { render } from 'svelte/server';
import { experimental_AstroContainer } from 'astro/container';
import { normalise } from './normalise';

export type Props = Record<string, unknown>;
export type Slots = Record<string, string>;

export interface Case {
  name: string;
  props?: Props;
  slots?: Slots;
  svelteProps?: Props;
  html?: string;
}

const container = await experimental_AstroContainer.create();

export const snippet = (html: string) => createRawSnippet(() => ({ render: () => html }));

export function svelteHtml(component: unknown, props: Props = {}, slots: Slots = {}, defaultSnippet = 'children'): string {
  const snippets = Object.fromEntries(
    Object.entries(slots).map(([name, html]) => [name === 'default' ? defaultSnippet : name, snippet(html)]),
  );
  return normalise(render(component as never, { props: { ...props, ...snippets } as never }).body);
}

export async function astroHtml(component: unknown, props: Props = {}, slots: Slots = {}): Promise<string> {
  return normalise(await container.renderToString(component as never, { props, slots }));
}

export async function bothHtml(svelte: unknown, astro: unknown, props: Props = {}, slots: Slots = {}): Promise<string> {
  const fromSvelte = svelteHtml(svelte, props, slots);
  expect(await astroHtml(astro, props, slots)).toBe(fromSvelte);
  return fromSvelte;
}

async function expectContract(svelte: unknown, astro: unknown, kase: Case, defaultSnippet: string): Promise<void> {
  const { props, slots, svelteProps, html } = kase;
  const fromSvelte = svelteHtml(svelte, { ...props, ...svelteProps }, slots, defaultSnippet);
  const fromAstro = await astroHtml(astro, props, slots);
  const expected = html ?? fromAstro;
  expect(fromSvelte).toBe(expected);
  expect(fromAstro).toBe(expected);
}

export function contracts(svelte: unknown, astro: unknown, cases: Case[], defaultSnippet = 'children'): void {
  for (const kase of cases) test(kase.name, () => expectContract(svelte, astro, kase, defaultSnippet));
}

type Pattern = string | RegExp;

export interface SourceCheck {
  file: string | string[];
  between?: [string, string];
  has?: Pattern[];
  lacks?: Pattern[];
  after?: [string, string][];
}

export interface SourceRule {
  name: string;
  strip?: boolean;
  checks: SourceCheck[];
}

const root = new URL('../', import.meta.url);

async function readSource(file: string | string[], strip: boolean): Promise<string> {
  const parts = await Promise.all([file].flat().map((path) => Bun.file(new URL(path, root)).text()));
  const text = parts.join('\n');
  return strip ? text.replace(/\/\*[\s\S]*?\*\//g, '') : text;
}

function slice(text: string, between?: [string, string]): string {
  if (!between) return text;
  const start = text.indexOf(between[0]);
  return text.slice(start, text.indexOf(between[1], start));
}

function expectPattern(text: string, pattern: Pattern, present: boolean): void {
  const subject = present ? expect(text) : expect(text).not;
  if (typeof pattern === 'string') subject.toContain(pattern);
  else subject.toMatch(pattern);
}

async function expectSource(check: SourceCheck, strip: boolean): Promise<void> {
  const text = slice(await readSource(check.file, strip), check.between);
  for (const pattern of check.has ?? []) expectPattern(text, pattern, true);
  for (const pattern of check.lacks ?? []) expectPattern(text, pattern, false);
  for (const [later, earlier] of check.after ?? []) expect(text.indexOf(later)).toBeGreaterThan(text.indexOf(earlier));
}

export function sourceContracts(rules: SourceRule[]): void {
  for (const rule of rules) {
    test(rule.name, async () => {
      for (const check of rule.checks) await expectSource(check, rule.strip ?? false);
    });
  }
}
