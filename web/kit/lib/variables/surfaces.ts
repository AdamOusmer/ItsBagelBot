// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { MODULE_CATALOG } from '../catalog';
import { builtinDef } from '../catalog/builtin-commands';
import type { ReplyToken } from '../catalog/module-def';
import { intactSpan } from '../engine/tmpl';
import { HINT_KEYS } from './hint-keys';
import { timerOwns } from '../engine/rehearsal';
import type { VariableDef, VariableForm, VariableGroup } from './types';
import { VARIABLES } from './variables';

export const CUSTOM_COMMAND_VARIABLES: readonly string[] = VARIABLES.map((v) => v.id);

const CUSTOM_COMMAND_SET = new Set(CUSTOM_COMMAND_VARIABLES);

const TIMER_VARIABLES: readonly string[] = VARIABLES.filter(
  (v) => timerOwns(v.head) || (v.aliases ?? []).some(timerOwns)
).map((v) => v.id);

const TIMER_SET = new Set(TIMER_VARIABLES);

const TRIGGERS_VARIABLE_IDS = new Set(['user', 'random', 'choice']);

export type VariableSurface =
  | 'custom'
  | 'timer'
  | 'triggers'
  | 'reward:channelpoints'
  | 'reward:spotify'
  | 'reward:govee'
  | { readonly module: string; readonly reply: string }
  | { readonly builtin: string };

const REWARD_TARGETS: Record<Extract<VariableSurface, `reward:${string}`>, { module: string; reply: string }> = {
  'reward:channelpoints': { module: 'channelpoints', reply: 'reply' },
  'reward:spotify': { module: 'songqueue', reply: 'redeem' },
  'reward:govee': { module: 'govee', reply: 'reply' }
};

const MANIFEST_SURFACE_SETS: Record<'custom' | 'timer' | 'triggers', ReadonlySet<string>> = {
  custom: CUSTOM_COMMAND_SET,
  timer: TIMER_SET,
  triggers: TRIGGERS_VARIABLE_IDS
};

function isManifestSurface(surface: VariableSurface): surface is 'custom' | 'timer' | 'triggers' {
  return typeof surface === 'string' && Object.hasOwn(MANIFEST_SURFACE_SETS, surface);
}

export function forSurface(surface: 'custom' | 'timer' | 'triggers'): readonly VariableDef[] {
  const set = MANIFEST_SURFACE_SETS[surface];
  return VARIABLES.filter((v) => set.has(v.id));
}

export interface VariableChip {
  readonly token: string;
  readonly hintKey?: string;
  readonly sample?: string;
  readonly id?: string;
  readonly requires?: string | null;
  readonly pinned?: boolean;
  readonly group?: VariableGroup;
  readonly replyOnly?: boolean;
}

function chipsOfVariable(v: VariableDef): VariableChip[] {
  const extra = (form: VariableForm): VariableChip[] =>
    form.chipHint
      ? [{ token: form.example, hintKey: `vars.${v.id}.${form.chipHint}`, id: v.id, requires: v.requires, group: v.group }]
      : [];
  return [
    { token: v.forms[0].example, hintKey: HINT_KEYS[v.id], pinned: v.pinned, id: v.id, requires: v.requires, group: v.group },
    ...v.forms.slice(1).flatMap(extra)
  ];
}

function chipOfReplyToken(tk: ReplyToken): VariableChip | null {
  const token = intactSpan(tk.name, null);
  return token === null ? null : { token, hintKey: tk.hintKey, sample: tk.sample, replyOnly: true };
}

function replyTokensFor(target: { module: string; reply: string } | { builtin: string }): readonly ReplyToken[] {
  if ('builtin' in target) return builtinDef(target.builtin)?.tokens ?? [];
  const mod = MODULE_CATALOG.find((m) => m.id === target.module);
  return mod?.replies.find((r) => r.key === target.reply)?.tokens ?? [];
}

export function chipsFor(surface: VariableSurface): readonly VariableChip[] {
  if (isManifestSurface(surface)) return forSurface(surface).flatMap(chipsOfVariable);
  const target = typeof surface === 'string' ? REWARD_TARGETS[surface] : surface;
  return replyTokensFor(target).flatMap((tk) => {
    const chip = chipOfReplyToken(tk);
    return chip ? [chip] : [];
  });
}

const PINNED_OVERRIDE: Readonly<Record<string, readonly string[]>> = {
  triggers: ['user', 'random', 'choice']
};

export function pinnedFor(surface: VariableSurface): readonly VariableChip[] {
  const chips = chipsFor(surface);
  const override = typeof surface === 'string' ? PINNED_OVERRIDE[surface] : undefined;
  if (override) {
    const byId = new Map(chips.map((c) => [c.id, c] as const));
    return override.flatMap((id) => {
      const chip = byId.get(id);
      return chip ? [chip] : [];
    });
  }
  const pinned = chips.filter((c) => c.pinned);
  return (pinned.length > 0 ? pinned : chips).slice(0, 6);
}

const PICKER_OWNED_IDS = new Set(['counter', 'urlfetch']);

const REPLY_WHO_IDS = ['user', 'channel'];
const REPLY_FUN_IDS = ['random', 'choice', 'math', 'countdown', 'countup', 'repeat', 'if'];
const REPLY_MANIFEST_IDS = new Set([...REPLY_WHO_IDS, ...REPLY_FUN_IDS]);
const REPLY_MANIFEST_CHIPS: readonly VariableChip[] = VARIABLES.filter((v) => REPLY_MANIFEST_IDS.has(v.id))
  .flatMap(chipsOfVariable)
  .map((c) => (c.id === 'channel' ? { ...c, group: 'who' as const } : c));

export function sheetFor(surface: VariableSurface): readonly VariableChip[] {
  const own = chipsFor(surface);
  if (surface === 'custom') return own.filter((c) => !c.id || !PICKER_OWNED_IDS.has(c.id));
  if (surface === 'timer') return own;
  const ownIds = new Set(own.map((c) => c.id).filter((id): id is string => !!id));
  return [...own, ...REPLY_MANIFEST_CHIPS.filter((c) => !c.id || !ownIds.has(c.id))];
}
