// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { definitionToFlow, type Flow } from '../flow-graph';
import { MAX_ACTIONS, MAX_DEFINITION_BYTES } from '../limits';
import type { Action, Definition } from '../types';

export function largeRecipe(): Definition {
  const message = 'Thanks {user} for being here! You can check your points with !points and join the next community game.';
  const actions: Action[] = [
    ...Array.from({ length: 96 }, () => ({ type: 'chat', params: { text: message } })),
    { type: 'delay', params: { ms: 1000 } },
    { type: 'var_set', params: { scope: 'channel', name: 'status', value: 'ready' } },
    { type: 'var_add', params: { scope: 'channel', name: 'points', amount: 10 } },
    { type: 'http_get', params: { url: 'https://example.com/status', save_as: 'result' } }
  ];
  return {
    version: 1,
    trigger: { type: 'event', params: { kind: 'channel.follow' } },
    conditions: [
      { type: 'chance', params: { percent: 90 } },
      { type: 'compare', params: { left: '{var:status}', op: 'eq', right: 'ready' } }
    ],
    actions: [{ type: 'parallel', params: { branches: Array.from({ length: 10 }, () => ({ weight: 1, actions })) } }]
  };
}

export function sizedRecipe(bytes = MAX_DEFINITION_BYTES): Definition {
  const definition: Definition = {
    version: 1,
    trigger: { type: 'event', params: { kind: 'channel.follow' } },
    actions: Array.from({ length: MAX_ACTIONS }, () => ({ type: 'chat', params: { text: '' } }))
  };
  const remaining = bytes - new TextEncoder().encode(JSON.stringify(definition)).length;
  const length = Math.floor(remaining / MAX_ACTIONS);
  definition.actions = definition.actions!.map((action, index) => ({ ...action, params: { text: 'x'.repeat(length + Number(index < remaining % MAX_ACTIONS)) } }));
  return definition;
}

export function largeEditorFlow(definition = largeRecipe()): Flow {
  const drawn = definitionToFlow(definition, { id: 'new', name: 'Community night', enabled: true });
  if (!drawn.ok) throw new Error(JSON.stringify(drawn.issues));
  const ids = new Map(drawn.value.nodes.map((node) => [node.id, crypto.randomUUID()]));
  return {
    ...drawn.value,
    nodes: drawn.value.nodes.map((node, index) => ({ ...node, id: ids.get(node.id)!, x: index * 240, y: index % 20 * 160 })),
    edges: drawn.value.edges.map((edge) => ({ ...edge, id: crypto.randomUUID(), from: ids.get(edge.from)!, to: ids.get(edge.to)! }))
  };
}
