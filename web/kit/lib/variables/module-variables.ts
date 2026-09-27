// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { lex, parseCond } from '../engine/tmpl';
import { MODULE_CATALOG } from '../catalog';
import { BUILTIN_COMMANDS } from '../catalog/builtin-commands';
import type { VariableDef } from './types';

// The same public reply palettes drive custom-command variables. The first
// view declaring a field supplies its short spelling; every view also has an
// explicit spelling so overlapping fields remain accessible.
export const MODULE_VARIABLE_SPECS = MODULE_CATALOG.map((mod) => ({
  id: mod.id,
  label: mod.label,
  groups: mod.replies.filter((r) => r.tokens?.length).map((r) => ({
    name: r.key.toLowerCase(),
    messageKey: r.messageKey as string | undefined,
    fields: r.tokens!.map((t) => t.name.replace(new RegExp(`^${mod.id}:`), '')),
    samples: Object.fromEntries(r.tokens!.map((t) => [t.name.replace(new RegExp(`^${mod.id}:`), ''), t.sample]))
  }))
}));

for (const def of BUILTIN_COMMANDS) {
  if (MODULE_VARIABLE_SPECS.some((mod) => mod.id === def.id) || !def.tokens?.length) continue;
  const strip = (field: string) => field.replace(new RegExp(`^${def.id}:`), '');
  MODULE_VARIABLE_SPECS.push({ id: def.id, label: def.label, groups: [{
    name: 'reply', messageKey: def.replyKey,
    fields: def.tokens.map((token) => strip(token.name)),
    samples: Object.fromEntries(def.tokens.map((token) => [strip(token.name), token.sample]))
  }] });
}

// Modules with fixed replies still publish their existing command facts.
const extraGroups: Record<string, { name: string; samples: Record<string, string> }[]> = {
  uptime: [{ name: 'reply', samples: { uptime: '2 hours, 15 minutes' } }],
  followage: [{ name: 'status', samples: { followage: '2 years, 3 months', followedat: '2024-06-01T12:00:00Z' } }],
  accountage: [{ name: 'status', samples: { accountage: '5 years, 2 months', createdat: '2021-07-01T12:00:00Z' } }],
  quotes: [{ name: 'quote', samples: { quote: 'Quote #12: bagels win (2026-01-31)', num: '12', text: 'bagels win', date: '2026-01-31' } }],
  songqueue: [{ name: 'current', samples: { song: 'Bagel Song by The Ovens', title: 'Bagel Song', artist: 'The Ovens', url: 'https://open.spotify.com/track/4uLU6hMCjMI75M1A2tKUQC', req: 'maya_live' } }, { name: 'redeem', samples: { user: 'maya_live', track: 'Bagel Song by The Ovens', input: 'Bagel Song', pos: '3' } }],
  loyalty: [{ name: 'balance', samples: { points: '1280', pointsname: 'crumbs', watchtime: '2 hours, 15 minutes', duration: '2 hours, 15 minutes', user: 'maya_live', name: 'crumbs', hours: '2' } }],
  stream: [{ name: 'channel', samples: { uptime: '2 hours, 15 minutes', title: 'bagel baking and chill', game: 'Just Chatting', viewers: '128' } }],
  queue: [{ name: 'status', samples: { size: '12', entries: 'maya_live, alex', open: 'true' } }],
  raffle: [{ name: 'status', samples: { entrants: '18', seconds: '120', open: 'true' } }],
  govee: [{ name: 'redeem', samples: { user: 'sesame_sam', color: 'blue' } }],
  channelpoints: [{ name: 'redeem', samples: { user: 'sesame_sam', reward: 'Hydrate', input: 'water please', cost: '500', channel: 'itsmavey', counter: '12', points: '100' } }],
  triggers: [{ name: 'response', samples: { user: 'sesame_sam', channel: 'itsmavey' } }],
  personality: [{ name: 'reply', samples: { user: 'sesame_sam' } }]
};

for (const id of Object.keys(extraGroups)) {
  if (!MODULE_VARIABLE_SPECS.some((mod) => mod.id === id)) {
    MODULE_VARIABLE_SPECS.push({ id, label: BUILTIN_COMMANDS.find((mod) => mod.id === id)?.label ?? id, groups: [] });
  }
}

for (const mod of MODULE_VARIABLE_SPECS) {
  for (const group of extraGroups[mod.id] ?? []) {
    const existing = mod.groups.find((candidate) => candidate.name === group.name);
    if (existing) {
      for (const [field, sample] of Object.entries(group.samples)) {
        if (!existing.fields.includes(field)) existing.fields.push(field);
        existing.samples[field] ??= sample;
      }
    } else {
      const publicGroup = { ...group, messageKey: undefined, fields: Object.keys(group.samples) };
      // The short raffle facts describe the current raffle; winner facts stay explicit.
      if (mod.id === 'raffle' && group.name === 'status') mod.groups.unshift(publicGroup);
      else mod.groups.push(publicGroup);
    }
  }
}

export const MODULE_VARIABLES: readonly VariableDef[] = MODULE_VARIABLE_SPECS
  .filter((mod) => mod.groups.length > 0)
  .map((mod) => {
    const seen = new Set<string>();
    const forms = mod.groups.flatMap((group) => group.fields.flatMap((field) => {
      const example = `{${mod.id}:${field}}`;
      const qualified = `{${mod.id}:${group.name}:${field}}`;
      const forms = seen.has(field) ? [] : [{ syntax: example, example, output: group.samples[field] }];
      seen.add(field);
      return [...forms, { syntax: qualified, example: qualified, output: group.samples[field] }];
    }));
    return { id: `module${mod.id}`, head: mod.id, group: 'data' as const, requires: mod.id, forms };
  });

export const MODULE_VARIABLE_SAMPLES = Object.fromEntries(MODULE_VARIABLES.flatMap((v) =>
  v.forms.map((f) => [f.example, f.output])
));

/** Module requirements include references used only by a conditional. */
export function requiredModuleVariables(template: string): readonly VariableDef[] {
  const heads = new Set<string>();
  for (const token of lex(template)) {
    if (token.kind !== 'var') continue;
    if (token.payload !== null) heads.add(token.name);
    const reference = parseCond(token)?.ref;
    if (reference?.payload !== null && reference?.payload !== undefined) heads.add(reference.name);
  }
  return MODULE_VARIABLES.filter((variable) => heads.has(variable.head));
}
