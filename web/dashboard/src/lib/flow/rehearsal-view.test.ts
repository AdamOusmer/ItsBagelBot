// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
import { expect, test } from 'bun:test';
import { newFlow } from './model';
import { rehearsalTimeline, rehearsalTrace, stepOutcome, type StepOutcome } from './rehearsal-view';
import type { RehearsalStep } from './rehearse';

const step = (index: number, extra: Partial<RehearsalStep> = {}): RehearsalStep => ({
  key: String(index), path: `actions[${index}]`, id: String(index), lane: [], iteration: [], atMs: 0,
  kind: 'effect', status: 'ok', label: '', text: '', chatLines: [], missing: [], sample: false, ...extra
});

test.each([0, 20, 1000])('timeline groups %i loop rows without changing the events', (count) => {
  const events = [step(-1, { kind: 'loop', detail: { times: String(count) } }), ...Array.from({ length: count }, (_, index) =>
    step(index, { path: 'actions[-1].params.actions[0]', iteration: [index + 1] })), step(count, { atMs: 5000 })];
  const groups = rehearsalTimeline(events);
  const first = groups[0].rows;
  expect([groups.map((group) => group.atMs), first.length, first.at(-1)?.more]).toEqual([[0, 5000], Math.min(count, 20) + 1, Math.max(0, count - 20)]);
  expect(events.length).toBe(count + 2);
});

test.each(['yes', 'no'])('trace follows the %s port and ignores skipped nodes', (port) => {
  const flow = { ...newFlow('Trace'), edges: ['yes', 'no'].map((arm) => ({ id: arm, from: '0', to: arm, port: arm })) };
  const events = [step(0, { port }), step(1, { id: port }), step(2, { id: 'skipped', status: 'skipped' })];
  const trace = rehearsalTrace(flow, events);
  expect([[...trace.nodes], [...trace.edges]]).toEqual([['0', port], [port]]);
});

test.each<{ extra: Partial<RehearsalStep> | null; outcome: StepOutcome }>([
  { extra: null, outcome: 'untested' },
  { extra: {}, outcome: 'passed' },
  { extra: { kind: 'check', status: 'yes' }, outcome: 'passed' },
  { extra: { kind: 'check', status: 'no' }, outcome: 'failed' },
  { extra: { status: 'failed' }, outcome: 'failed' },
  { extra: { status: 'skipped' }, outcome: 'skipped' },
  { extra: { kind: 'trigger', status: 'skipped' }, outcome: 'failed' },
  { extra: { issues: [{ nodeId: '0', fieldKey: 'message', code: 'required', path: 'actions[0].params.text' }] }, outcome: 'issue' }
])('step outcome for $extra is $outcome', ({ extra, outcome }) => {
  expect(stepOutcome(extra ? step(0, extra) : undefined)).toBe(outcome);
});
