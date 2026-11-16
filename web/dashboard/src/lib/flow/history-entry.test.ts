// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
import { expect, test } from 'bun:test';
import { FlowHistory } from './history';
import { newFlow, type Flow } from './model';
import { addNode, deleteNode } from './ops';

type Change = (flow: Flow, history: FlowHistory) => void;
const changes: { name: string; change: Change; current: boolean }[] = [
  { name: 'no edit', change: (flow, history) => { history.sync(flow); }, current: true },
  { name: 'tidy', change: (flow, history) => { flow.nodes[0].x += 24; history.sync(flow); }, current: true },
  { name: 'config edit', change: (flow, history) => { flow.nodes[0].config.trigger = '!new'; history.sync(flow); }, current: false },
  { name: 'add', change: (flow, history) => { addNode(flow, 'reply.wait', { x: 0, y: 0 }); history.sync(flow); }, current: false },
  { name: 'move', change: (_flow, history) => { history.moved([{ id: 't', from: { x: 0, y: 0 }, to: { x: 24, y: 0 } }]); }, current: false },
  { name: 'undo', change: (flow, history) => { history.undo(flow); }, current: false },
  { name: 'redo another entry', change: (flow, history) => { history.undo(flow); history.undo(flow); history.redo(flow); }, current: false }
];

test.each(changes)('deletion undo entry after $name', ({ change, current }) => {
  const flow = newFlow('History');
  const root = addNode(flow, 'twitch.chat.command', { x: 0, y: 0 });
  const history = new FlowHistory(flow);
  const node = addNode(flow, 'reply.wait', { x: 360, y: 0 }, { from: root.id, port: 'next' });
  history.sync(flow);
  deleteNode(flow, node.id);
  history.sync(flow);
  const deletion = history.undoEntry;
  change(flow, history);
  expect(history.undoEntry === deletion).toBe(current);
});
