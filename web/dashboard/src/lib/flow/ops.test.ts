// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
import { describe, expect, spyOn, test } from 'bun:test';
import { definitionToFlow, flowToDefinition } from '@bagel/kit/automations';
import { nodeHeight, tidy } from './layout';
import { specFor, type Flow } from './model';
import { addNode, duplicateNode } from './ops';

const at = { x: 0, y: 0 };

describe('adding steps', () => {
  test('same-type sequential and branch steps stay distinct through saving and reopening', () => {
    const flow: Flow = { id: 'f', name: 'flow', enabled: true, nodes: [], edges: [] };
    const trigger = addNode(flow, 'twitch.follow', at);
    const first = addNode(flow, 'reply.chat', at, { from: trigger.id, port: 'next' });
    first.config.message = 'first';
    const second = addNode(flow, 'reply.chat', at, { from: first.id, port: 'next' });
    second.config.message = 'second';
    const gate = addNode(flow, 'cond.chance', at, { from: second.id, port: 'next' });
    gate.config.percent = 50;
    const yes = addNode(flow, 'reply.chat', at, { from: gate.id, port: 'yes' });
    yes.config.message = 'yes';
    const no = addNode(flow, 'reply.chat', at, { from: gate.id, port: 'no' });
    no.config.message = 'no';
    expect(new Set(flow.nodes.map((node) => node.id)).size).toBe(6);
    const positions = tidy(flow);
    expect(positions.get(first.id)!.x).toBeLessThan(positions.get(second.id)!.x);
    expect(Math.abs(positions.get(yes.id)!.y - positions.get(no.id)!.y)).toBeGreaterThanOrEqual(nodeHeight(specFor(yes.type)));

    const saved = flowToDefinition(flow);
    if (!saved.ok) throw new Error(JSON.stringify(saved.issues));
    const opened = definitionToFlow(saved.value, flow);
    if (!opened.ok) throw new Error(JSON.stringify(opened.issues));
    const reopened = opened.value;
    const getMessage = (id: string) => reopened.nodes.find((node) => node.id === id)?.config.message;
    const outgoing = (id: string, port: string) => reopened.edges.find((edge) => edge.from === id && edge.port === port)!.to;
    const root = reopened.nodes.find((node) => node.type === 'twitch.follow')!;
    const a = outgoing(root.id, 'next');
    const b = outgoing(a, 'next');
    const c = outgoing(b, 'next');
    expect([getMessage(a), getMessage(b), getMessage(outgoing(c, 'yes')), getMessage(outgoing(c, 'no'))]).toEqual(['first', 'second', 'yes', 'no']);
    expect(new Set(reopened.nodes.map((node) => node.id)).size).toBe(6);
    const reopenedPositions = tidy(reopened);
    expect(reopenedPositions.get(outgoing(c, 'yes'))).not.toEqual(reopenedPositions.get(outgoing(c, 'no')));
  });

  test('ID collisions do not merge newly added or duplicated steps and edges', () => {
    const uuid = spyOn(globalThis.crypto, 'randomUUID').mockReturnValue('00000000-0000-4000-8000-000000000000');
    try {
      const flow: Flow = { id: 'f', name: 'flow', enabled: true, nodes: [], edges: [] };
      const root = addNode(flow, 'twitch.follow', at);
      const first = addNode(flow, 'reply.chat', at, { from: root.id, port: 'next' });
      const second = addNode(flow, 'reply.chat', at, { from: first.id, port: 'next' });
      const copy = duplicateNode(flow, second.id)!;
      expect(new Set(flow.nodes.map((node) => node.id)).size).toBe(4);
      expect(new Set(flow.edges.map((edge) => edge.id)).size).toBe(2);
      expect(copy.id).not.toBe(second.id);
      expect(flow.edges.map(({ from, to }) => [from, to])).toEqual([[root.id, first.id], [first.id, second.id]]);
    } finally {
      uuid.mockRestore();
    }
  });
});
