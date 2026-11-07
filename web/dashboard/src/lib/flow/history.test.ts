// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
import { describe, expect, test } from 'bun:test';
import { COALESCE_MS, FlowHistory } from './history';
import type { Flow } from './model';
import { addNode, connect, duplicateNode, removeEdge, removeNode } from './ops';

const fresh = (): Flow => ({
  id: 'f',
  name: 'f',
  enabled: true,
  nodes: [{ id: 't', type: 'twitch.chat.command', x: 0, y: 0, config: { trigger: '!hug' } }],
  edges: []
});

const ids = (f: Flow) => f.nodes.map((n) => n.id);

describe('FlowHistory', () => {
  test('undo and redo walk back through add, connect, disconnect, duplicate and remove', () => {
    const flow = fresh();
    const h = new FlowHistory(flow);
    let t = 0;
    const step = (mutate: () => void) => {
      mutate();
      h.sync(flow, (t += COALESCE_MS * 2));
    };

    const reply = addNode(flow, 'reply.chat', { x: 368, y: 0 });
    h.sync(flow, (t += COALESCE_MS * 2));
    step(() => void connect(flow, { from: 't', port: 'next', to: reply.id }));
    step(() => removeEdge(flow, flow.edges[0].id));
    step(() => void duplicateNode(flow, reply.id));
    step(() => removeNode(flow, reply.id));

    expect(ids(flow)).toHaveLength(2);
    h.undo(flow);
    expect(ids(flow)).toHaveLength(3);
    h.undo(flow);
    expect(ids(flow)).toHaveLength(2);
    expect(flow.edges).toHaveLength(0);
    h.undo(flow);
    expect(flow.edges).toHaveLength(1);
    h.undo(flow);
    expect(flow.edges).toHaveLength(0);
    h.undo(flow);
    expect(ids(flow)).toEqual(['t']);
    expect(h.undo(flow)).toBeNull();

    h.redo(flow);
    expect(ids(flow)).toHaveLength(2);
    for (let i = 0; i < 4; i++) h.redo(flow);
    expect(ids(flow)).toHaveLength(2);
    expect(flow.edges).toHaveLength(0);
    expect(h.redo(flow)).toBeNull();
  });

  test('a new edit clears the redo stack', () => {
    const flow = fresh();
    const h = new FlowHistory(flow);
    addNode(flow, 'reply.chat', { x: 0, y: 0 });
    h.sync(flow, 0);
    h.undo(flow);
    expect(h.canRedo).toBe(true);
    addNode(flow, 'reply.wait', { x: 0, y: 0 });
    h.sync(flow, 10);
    expect(h.canRedo).toBe(false);
  });

  test('typing into one field is a single entry until the pause', () => {
    const flow = fresh();
    const h = new FlowHistory(flow);
    for (const [i, text] of ['!h', '!hu', '!hug2'].entries()) {
      flow.nodes[0].config.trigger = text;
      h.sync(flow, 5000 + i * 100);
    }
    h.undo(flow);
    expect(flow.nodes[0].config.trigger).toBe('!hug');
    expect(h.canUndo).toBe(false);

    h.redo(flow);
    flow.nodes[0].config.trigger = '!pat';
    h.sync(flow, 9000);
    h.undo(flow);
    expect(flow.nodes[0].config.trigger).toBe('!hug2');
  });

  test('different fields do not coalesce', () => {
    const flow = fresh();
    flow.nodes.push({ id: 'r', type: 'reply.chat', x: 368, y: 0, config: {} });
    const h = new FlowHistory(flow);
    flow.nodes[1].config.channel = 'general';
    h.sync(flow, 100);
    flow.nodes[1].config.message = 'hi';
    h.sync(flow, 200);
    h.undo(flow);
    expect(flow.nodes[1].config).toEqual({ channel: 'general' });
    h.undo(flow);
    expect(flow.nodes[1].config).toEqual({});
  });

  test('typing back to the start leaves nothing to undo', () => {
    const flow = fresh();
    const h = new FlowHistory(flow);
    flow.nodes[0].config.trigger = '!x';
    h.sync(flow, 100);
    flow.nodes[0].config.trigger = '!hug';
    h.sync(flow, 200);
    expect(h.canUndo).toBe(false);
  });

  test('manual moves undo, but tidy repositioning is never recorded', () => {
    const flow = fresh();
    const h = new FlowHistory(flow);

    flow.nodes[0].x = 240;
    expect(h.sync(flow, 100)).toBe(false);
    expect(h.canUndo).toBe(false);

    h.moved([{ id: 't', from: { x: 240, y: 0 }, to: { x: 288, y: 48 } }], 200);
    flow.nodes[0].x = 288;
    flow.nodes[0].y = 48;
    expect(h.undo(flow)).toBe('move');
    expect(flow.nodes[0]).toMatchObject({ x: 240, y: 0 });
    h.redo(flow);
    expect(flow.nodes[0]).toMatchObject({ x: 288, y: 48 });
  });

  test('repeated nudges of one step merge, and no-op moves are ignored', () => {
    const flow = fresh();
    const h = new FlowHistory(flow);
    h.moved([{ id: 't', from: { x: 0, y: 0 }, to: { x: 0, y: 0 } }], 100);
    expect(h.canUndo).toBe(false);
    h.moved([{ id: 't', from: { x: 0, y: 0 }, to: { x: 24, y: 0 } }], 200);
    h.moved([{ id: 't', from: { x: 24, y: 0 }, to: { x: 48, y: 0 } }], 300);
    flow.nodes[0].x = 48;
    h.undo(flow);
    expect(flow.nodes[0].x).toBe(0);
    expect(h.canUndo).toBe(false);
  });

  test('undoing a config edit keeps tidy positions, undoing a removal restores the step', () => {
    const flow = fresh();
    const h = new FlowHistory(flow);
    flow.nodes[0].config.trigger = '!pat';
    h.sync(flow, 100);
    flow.nodes[0].x = 480;
    h.undo(flow);
    expect(flow.nodes[0]).toMatchObject({ x: 480, config: { trigger: '!hug' } });

    const reply = addNode(flow, 'reply.chat', { x: 368, y: 24 });
    h.sync(flow, 5000);
    removeNode(flow, reply.id);
    h.sync(flow, 9000);
    h.undo(flow);
    expect(flow.nodes.find((n) => n.id === reply.id)).toMatchObject({ x: 368, y: 24 });
  });

  test('undo and redo do not record themselves', () => {
    const flow = fresh();
    const h = new FlowHistory(flow);
    addNode(flow, 'reply.chat', { x: 0, y: 0 });
    h.sync(flow, 0);
    h.undo(flow);
    expect(h.sync(flow, 10)).toBe(false);
    h.redo(flow);
    expect(h.sync(flow, 20)).toBe(false);
    expect(h.canRedo).toBe(false);
  });
});
