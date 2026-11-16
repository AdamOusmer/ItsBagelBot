// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
import { expect, test } from 'bun:test';
import { newFlow } from './model';
import { removeCheckKeepYes } from './ops';
import { FlowHistory } from './history';

type EdgeRow = [string, string, string];
const cases: { name: string; edges: EdgeRow[]; remaining: EdgeRow[]; lost: string[] }[] = [
  { name: 'reward and subscriber check', edges: [['reward', 'next', 'role'], ['role', 'yes', 'YES'], ['role', 'no', 'NO']], remaining: [['reward', 'next', 'YES']], lost: ['NO'] },
  { name: 'every incoming edge and Yes child keeps its order', edges: [['reward', 'next', 'role'], ['reward', 'next', 'parent'], ['parent', 'next', 'role'], ['role', 'yes', 'YES'], ['role', 'yes', 'second'], ['role', 'no', 'NO']], remaining: [['reward', 'next', 'YES'], ['reward', 'next', 'second'], ['reward', 'next', 'parent'], ['parent', 'next', 'YES'], ['parent', 'next', 'second']], lost: ['NO'] },
  { name: 'shared No descendant stays', edges: [['reward', 'next', 'role'], ['role', 'yes', 'YES'], ['role', 'no', 'NO'], ['NO', 'next', 'shared'], ['YES', 'next', 'shared']], remaining: [['reward', 'next', 'YES'], ['YES', 'next', 'shared']], lost: ['NO'] },
  { name: 'No path reached elsewhere stays', edges: [['reward', 'next', 'role'], ['role', 'yes', 'YES'], ['role', 'no', 'NO'], ['reward', 'next', 'NO']], remaining: [['reward', 'next', 'YES'], ['reward', 'next', 'NO']], lost: [] },
  { name: 'empty Yes path', edges: [['reward', 'next', 'role'], ['role', 'no', 'NO'], ['NO', 'next', 'tail']], remaining: [], lost: ['NO', 'tail'] },
  { name: 'a Yes child already linked from the parent keeps the Yes order', edges: [['reward', 'next', 'role'], ['reward', 'next', 'Y'], ['role', 'yes', 'Y'], ['role', 'yes', 'Z'], ['role', 'no', 'NO']], remaining: [['reward', 'next', 'Y'], ['reward', 'next', 'Z']], lost: ['NO'] },
  { name: 'both branches join',edges: [['reward', 'next', 'role'], ['role', 'yes', 'shared'], ['role', 'no', 'shared']], remaining: [['reward', 'next', 'shared']], lost: [] }
];

test.each(cases)('removeCheckKeepYes: $name', ({ edges, remaining, lost }) => {
  const ids = [...new Set(edges.flatMap(([from, , to]) => [from, to]))];
  const flow = {
    ...newFlow('Reward'),
    nodes: ids.map((id) => ({ id, type: id === 'reward' ? 'twitch.reward' : id === 'role' ? 'cond.role' : 'reply.chat', x: 0, y: 0, config: { message: id, level: 'sub' } })),
    edges: edges.map(([from, port, to], index) => ({ id: `e${index}`, from, port, to }))
  };
  const before = structuredClone(flow);
  const history = new FlowHistory(flow);
  const result = removeCheckKeepYes(flow, 'role');
  history.sync(flow);
  expect({ edges: flow.edges.map(({ from, port, to }) => [from, port, to]), removed: result.removed }).toEqual({ edges: remaining, removed: ['role', ...lost] });
  history.undo(flow);
  expect({ flow, furtherUndo: history.canUndo }).toEqual({ flow: before, furtherUndo: false });
});

test('removing an absent check leaves the graph alone', () => {
  const flow = newFlow('Empty');
  expect(removeCheckKeepYes(flow, 'missing')).toEqual({ removed: [] });
});
