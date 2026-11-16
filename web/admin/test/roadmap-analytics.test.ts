// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { expect, test } from 'bun:test';
import { buildAnalytics, type AnalyticsReads } from '../src/lib/analytics-page';
import type { AnalyticsRow } from '../src/lib/analytics-view';
import { readConsoleTree } from '../../kit/lib/i18n/tree-fs';

const at = '2026-11-16';
const row = (metric: string, dims: string[], value: number, start = at): AnalyticsRow => ({ metric, dims, value, start });
const reads: AnalyticsReads = {
  end: at, days: 1, week: [], month: [], cohortWeeks: [], cohortDays: [],
  counters: [
    row('timers.ticks', ['fired'], 5), row('timers.ticks', ['gated'], 2),
    row('games.rounds', ['mraffle', 'empty'], 1), row('games.rounds', ['duel', 'won'], 3),
    row('games.entrants', ['raffle', '20_99'], 7), row('points.flows', ['transfer'], 4),
    row('rewards.redeems', ['govee', 'bad_input'], 2), row('automod.flags', ['lex_hate', 'shadow'], 6),
    row('auth.login', ['started'], 10), row('auth.login', ['ok'], 8),
    row('lifecycle', ['trial_promoted'], 3), row('auth.login', ['ok'], 100, '2026-11-15')
  ],
  levels: [
    row('timers.total', ['gated'], 9), row('accounts.runtime', ['degraded'], 2), row('desktop.grants', ['macos'], 4),
    row('timers.total', ['gated'], 200, '2026-11-15')
  ]
};

test('uses the selected day for counters and the latest snapshot for levels', () => {
  const view = buildAnalytics(reads);
  expect({
    ticks: view.community.ticks.map((b) => b.value), timers: view.community.timers.map((b) => b.value),
    rounds: view.community.rounds.rows.find((r) => r.id === 'duel')?.cells.map((c) => c.text),
    entrants: view.community.entrants.rows[0].cells.map((c) => c.text),
    rewards: view.community.rewards.rows[1].cells.map((c) => c.text),
    points: view.community.points.find((b) => b.id === 'transfer')?.value,
    moderation: view.reliability.flags.rows.find((r) => r.id === 'lex_hate')?.cells.map((c) => c.text),
    login: view.growth.login.slice(0, 2).map((b) => b.value), trials: view.growth.trials,
    runtime: view.audience.runtime.find((b) => b.id === 'degraded')?.value, desktop: view.audience.desktop[0].value
  }).toEqual({
    ticks: [5, 2, 0, 0, 0, 0], timers: [0, 9, 0, 0, 0], rounds: ['0', '0', '0', '0', '0', '3', '0'],
    entrants: ['0', '0', '0', '7', '0'], rewards: ['0', '0', '2', '0', '0', '0'], points: 4,
    moderation: ['6', '0'], login: [10, 8], trials: [{ key: 'trialPromoted', value: '3' }], runtime: 2, desktop: 4
  });
});

function lookup(tree: unknown, key: string): unknown {
  return key.split('.').reduce((node: any, part) => node?.[part], tree);
}

test.each(['en', 'de', 'es', 'fr', 'pt-br', 'ru'])('resolves every new view label in %s', (locale) => {
  const tree = readConsoleTree(locale);
  const view = buildAnalytics(reads);
  const names = [...view.community.ticks, ...view.community.timers, ...view.community.points,
    ...view.growth.login, ...view.audience.runtime, ...view.audience.desktop,
    ...view.community.rounds.rows, ...view.community.rewards.rows, ...view.reliability.flags.rows];
  const keys = names.map((r) => 'key' in r.name ? r.name.key : '').filter(Boolean);
  const cards = ['ticks', 'timers', 'rounds', 'entrants', 'points', 'rewards', 'login', 'trials', 'moderation', 'runtime', 'desktop'];
  keys.push(...cards.flatMap((id) => [`admin.analytics.card.${id}`, `admin.analytics.hint.${id}`]));
  expect(keys.every((key) => typeof lookup(tree, key) === 'string')).toBe(true);
});
