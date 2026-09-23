// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Which Variables a Surface (CONTEXT.md "Language: reply templates") offers.
// 'custom' is the manifest itself; every other surface reads a module's
// ModuleReply.tokens or a built-in command's tokens off the catalog, so a
// dashboard editor never hand-keeps its own copy of a palette
// (docs/specs/variables-catalog.md section 5, phase 4: "the manifest is the
// only TS inventory").

import { MODULE_CATALOG } from '../catalog';
import { builtinDef } from '../catalog/builtin-commands';
import type { ReplyToken } from '../catalog/module-def';
import { intactSpan } from '../engine/tmpl';
import { HINT_KEYS } from './hint-keys';
import { timerOwns } from '../engine/rehearsal';
import type { VariableDef, VariableForm } from './types';
import { VARIABLES } from './variables';

/** Every Variable id the custom-command surface offers, in VARIABLES order. */
export const CUSTOM_COMMAND_VARIABLES: readonly string[] = VARIABLES.map((v) => v.id);

const CUSTOM_COMMAND_SET = new Set(CUSTOM_COMMAND_VARIABLES);

/** Every Variable a timer's message can resolve, in VARIABLES order —
 * whichever of a Variable's head or aliases engine/rehearsal.ts's timerOwns
 * claims. That function is Go's timerChain mirrored on this side (see its
 * own comment for which scopes a tick mounts and why); parity.test.ts checks
 * the id set this produces against app/twitch/sesame/engine/scope/testdata/
 * token_catalog.golden.json's surfaces.timer list, so the two cannot drift. */
const TIMER_VARIABLES: readonly string[] = VARIABLES.filter(
  (v) => timerOwns(v.head) || (v.aliases ?? []).some(timerOwns)
).map((v) => v.id);

const TIMER_SET = new Set(TIMER_VARIABLES);

// TriggerRuleEditor's palette is {user}, {random} and {choice:a,b,c}: three
// ordinary custom-command Variables, not a module-private substitution list.
// See catalog/triggers.ts's `replies: []` comment for why this reuses the
// manifest instead of inventing a ReplyToken palette that could not carry
// {choice}'s worked payload example.
const TRIGGERS_VARIABLE_IDS = new Set(['user', 'random', 'choice']);

/** Which Variables (or module-reply tokens) one Surface offers. Only the
 * manifest-backed surfaces ('custom', 'triggers') return VariableDefs;
 * reward and generic module/builtin surfaces are ReplyToken-shaped and are
 * read directly by chipsFor below. */
export type VariableSurface =
  | 'custom'
  | 'timer'
  | 'triggers'
  | 'reward:channelpoints'
  | 'reward:spotify'
  | 'reward:govee'
  | { readonly module: string; readonly reply: string }
  | { readonly builtin: string };

/** The {module, reply} pair a 'reward:<x>' literal is sugar for. Spotify's
 * reward IS the songqueue module's 'redeem' reply (SpotifyRewardEditor edits
 * songqueue's replyMessage config key); there is no separate 'spotify'
 * module in the catalog. Keyed by every `reward:${string}` arm of
 * VariableSurface rather than `Record<string, …>`, so adding a new
 * 'reward:x' literal to the union without a matching entry here is a
 * compile error, not a chip strip that silently renders empty. */
const REWARD_TARGETS: Record<Extract<VariableSurface, `reward:${string}`>, { module: string; reply: string }> = {
  'reward:channelpoints': { module: 'channelpoints', reply: 'reply' },
  'reward:spotify': { module: 'songqueue', reply: 'redeem' },
  'reward:govee': { module: 'govee', reply: 'reply' }
};

/** The Variables one manifest-backed Surface offers, in VARIABLES order.
 * 'timer' is the subset a timer's message can resolve (see TIMER_VARIABLES). */
const MANIFEST_SURFACE_SETS: Record<'custom' | 'timer' | 'triggers', ReadonlySet<string>> = {
  custom: CUSTOM_COMMAND_SET,
  timer: TIMER_SET,
  triggers: TRIGGERS_VARIABLE_IDS
};

/** Whether a Surface is answered from the manifest (forSurface) rather than
 * from a module reply's own tokens. Keyed off MANIFEST_SURFACE_SETS so a
 * new manifest-backed surface is one table row, not another `||` arm. */
function isManifestSurface(surface: VariableSurface): surface is 'custom' | 'timer' | 'triggers' {
  return typeof surface === 'string' && Object.hasOwn(MANIFEST_SURFACE_SETS, surface);
}

export function forSurface(surface: 'custom' | 'timer' | 'triggers'): readonly VariableDef[] {
  const set = MANIFEST_SURFACE_SETS[surface];
  return VARIABLES.filter((v) => set.has(v.id));
}

/** One dashboard insert chip: the literal text it inserts and, when one
 * exists, the locale key of its tooltip (absent chips fall back to the
 * caller's own copy, same as ResponseEditor's chipTitle already does).
 * `pinned` carries VariableDef.pinned through for the capped surfaces
 * (ResponseEditor, CommandBuilder.astro) to filter on; it is only ever true
 * on the first-form chip of a manifest Variable, never on an extra chipHint
 * form or a module-reply chip. */
export interface VariableChip {
  readonly token: string;
  readonly hintKey?: string;
  readonly pinned?: boolean;
}

function chipsOfVariable(v: VariableDef): VariableChip[] {
  const extra = (form: VariableForm): VariableChip[] =>
    form.chipHint ? [{ token: form.example, hintKey: `vars.${v.id}.${form.chipHint}` }] : [];
  return [{ token: v.forms[0].example, hintKey: HINT_KEYS[v.id], pinned: v.pinned }, ...v.forms.slice(1).flatMap(extra)];
}

// A ReplyToken's bare name is minted into a chat span the same way
// ReplyEditor.svelte's palette already did: through intactSpan rather than
// string interpolation, so a name carrying '}' or '|' is dropped instead of
// offered as a chip that reads right and resolves to something else.
function chipOfReplyToken(tk: ReplyToken): VariableChip | null {
  const token = intactSpan(tk.name, null);
  return token === null ? null : { token, hintKey: tk.hintKey };
}

function replyTokensFor(target: { module: string; reply: string } | { builtin: string }): readonly ReplyToken[] {
  if ('builtin' in target) return builtinDef(target.builtin)?.tokens ?? [];
  const mod = MODULE_CATALOG.find((m) => m.id === target.module);
  return mod?.replies.find((r) => r.key === target.reply)?.tokens ?? [];
}

/** The chip strip one Surface shows, in catalog order. Shared by the
 * dashboard's reward/reply editors and web/kit/lib/variables/surfaces.test.ts
 * so the test checks the strip the components render rather than a
 * re-implementation of it. */
export function chipsFor(surface: VariableSurface): readonly VariableChip[] {
  if (isManifestSurface(surface)) return forSurface(surface).flatMap(chipsOfVariable);
  const target = typeof surface === 'string' ? REWARD_TARGETS[surface] : surface;
  return replyTokensFor(target).flatMap((tk) => {
    const chip = chipOfReplyToken(tk);
    return chip ? [chip] : [];
  });
}
