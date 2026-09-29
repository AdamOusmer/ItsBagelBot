import { describe, expect, test } from 'bun:test';
import type { ImportFailedItem, PreviewResponse } from '@bagel/kit';
import {
  buildRetryManifest,
  buildSelectedManifest,
  buildSelection,
  capSkipped,
  countFatal,
  countPicked,
  countRows,
  isChecked,
  itemDiags,
  mergeRetry
} from './helpers';

const preview: PreviewResponse = {
  stats: { commands: 2, timers: 1, triggers: 0, quotes: 0 },
  manifest: {
    commands: [
      { name: 'a', responses: ['x'] },
      { name: 'b', responses: ['y'] }
    ],
    timers: [{ message: 'hi', interval_seconds: 300 }],
    automod: { block: ['bad'] }
  } as PreviewResponse['manifest'],
  diagnostics: [
    { item_index: 1, code: 'command_bad', severity: 'error', message: 'nope' },
    { item_index: 0, code: 'timer_warn', severity: 'warn', message: 'meh' },
    { item_index: -1, code: 'manifest', severity: 'warn', message: 'top' }
  ] as PreviewResponse['diagnostics']
};

describe('selection helpers', () => {
  test('itemDiags filters by kind prefix and index', () => {
    expect(itemDiags(preview.diagnostics, 'commands', 1)).toHaveLength(1);
    expect(itemDiags(preview.diagnostics, 'timers', 1)).toHaveLength(0);
    expect(itemDiags(undefined, 'commands', 0)).toEqual([]);
  });

  test('buildSelection unchecks fatal rows and honours none', () => {
    const all = buildSelection(preview, true);
    expect(all['commands:0']).toBe(true);
    expect(all['commands:1']).toBe(false);
    expect(all['timers:0']).toBe(true);
    expect(Object.values(buildSelection(preview, false)).some(Boolean)).toBe(false);
    expect(buildSelection(null, true)).toEqual({});
  });

  test('counts', () => {
    const sel = buildSelection(preview, true);
    expect(countRows(preview.manifest)).toBe(3);
    expect(countPicked(preview.manifest, sel)).toBe(2);
    expect(countFatal(preview)).toBe(1);
    expect(isChecked({}, 'commands', 9)).toBe(true);
  });

  test('buildSelectedManifest keeps only picked rows', () => {
    const sel = buildSelection(preview, true);
    const out = JSON.parse(buildSelectedManifest(preview, sel));
    expect(out.commands).toHaveLength(1);
    expect(out.commands[0].name).toBe('a');
    expect(out.timers).toHaveLength(1);
    expect(out.automod).toEqual({ block: ['bad'] });
  });

  test('buildSelectedManifest is empty when nothing is picked', () => {
    expect(buildSelectedManifest(preview, buildSelection(preview, false))).toBe('{}');
    expect(buildSelectedManifest(null, {})).toBe('{}');
  });

  test('capSkipped bounds the list and reports the remainder', () => {
    const skipped = Array.from({ length: 11 }, (_, i) => ({ kind: 'command', name: `c${i}` }));
    const capped = capSkipped(skipped);
    expect(capped.shown).toHaveLength(8);
    expect(capped.more).toBe(3);
    expect(capSkipped(undefined)).toEqual({ shown: [], more: 0 });
  });
});

describe('retry manifest', () => {
  const sent = JSON.stringify({
    commands: [{ name: '!Hello' }, { name: 'bye' }],
    timers: [{ message: ' tick ', interval_seconds: 300 }],
    triggers: [{ phrase: 'hi', response: 'yo' }],
    quotes: [{ text: 'q1' }],
    automod: { block: ['x'] }
  });

  const cases: [string, ImportFailedItem[] | undefined, Record<string, unknown>][] = [
    ['nothing failed', undefined, {}],
    ['keeps only failed commands', [{ kind: 'command', name: 'hello', reason: 'rejected' }], { commands: [{ name: '!Hello' }] }],
    ['module failures across kinds', [
      { kind: 'timer', name: 'tick', reason: 'module' },
      { kind: 'automod', name: 'automod', reason: 'module' }
    ], { timers: [{ message: ' tick ', interval_seconds: 300 }], automod: { block: ['x'] } }],
    ['invalid items are not retried', [{ kind: 'trigger', name: 'hi', reason: 'invalid' }], {}],
    ['unknown names match nothing', [{ kind: 'quote', name: 'zzz', reason: 'rejected' }], {}]
  ];

  test.each(cases)('%s', (_, failed, want) => {
    const got = buildRetryManifest(sent, failed);
    expect(got === '{}' ? {} : JSON.parse(got)).toEqual(want);
  });

  test('mergeRetry sums applied and replaces failures', () => {
    const prev = { applied: { commands: 2, timers: 0, triggers: 0, quotes: 1 }, skipped: [{ kind: 'command', name: 'a' }], failed: [{ kind: 'command' as const, name: 'b', reason: 'rejected' as const }] };
    const next = { applied: { commands: 1, timers: 0, triggers: 0, quotes: 0 }, failed: undefined };
    const merged = mergeRetry(prev, next);
    expect(merged.applied).toEqual({ commands: 3, timers: 0, triggers: 0, quotes: 1 });
    expect(merged.skipped).toEqual(prev.skipped);
    expect(merged.failed).toBeUndefined();
  });
});
