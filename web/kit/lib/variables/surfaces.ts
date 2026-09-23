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
import type { VariableDef, VariableForm, VariableGroup } from './types';
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
 * caller's own copy: VariablePalette's row falls back to `"{token} → sample"`
 * built from `sample`, same as ResponseEditor's old chipTitle used to).
 *
 * Two shapes share this interface rather than a union, because VariablePalette
 * walks one flat list and only needs to branch on `replyOnly`:
 *
 *   - a manifest-backed chip (VARIABLES, via chipsOfVariable) carries `id`,
 *     `requires` and `group` from its VariableDef, plus `pinned` for the
 *     capped surfaces (ResponseEditor, CommandBuilder.astro) to filter on —
 *     only ever true on the first-form chip of a Variable, never on an extra
 *     chipHint form. These sort into the sheet's five group sections.
 *   - a module-reply chip (ReplyToken, via chipOfReplyToken) carries
 *     `replyOnly: true` and no `id`/`requires`/`group` to sort by, so it
 *     lands in the sheet's "This reply" section instead — see module-def.ts's
 *     ReplyToken for why a reward or module reply cannot resolve an
 *     arbitrary manifest token (its rehearsal chain mounts only PURE_SCOPE
 *     plus its own token map). It still carries `sample` (ReplyToken.sample),
 *     for the same title-fallback reason a manifest chip's `output` doesn't
 *     need to: a manifest chip always has a locale hint (HINT_KEYS covers
 *     every id), a ReplyToken does not. */
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

// A ReplyToken's bare name is minted into a chat span the same way
// ReplyEditor.svelte's palette already did: through intactSpan rather than
// string interpolation, so a name carrying '}' or '|' is dropped instead of
// offered as a chip that reads right and resolves to something else.
function chipOfReplyToken(tk: ReplyToken): VariableChip | null {
  const token = intactSpan(tk.name, null);
  return token === null ? null : { token, hintKey: tk.hintKey, sample: tk.sample, replyOnly: true };
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

/** Per-surface pinned-chip overrides for the one surface whose row is not
 * simply "every VariableDef.pinned entry it offers": 'user' is pinned GLOBALLY
 * (shared with 'custom'), so filtering triggers' three chips (user, random,
 * choice) by that flag alone would starve its row down to the one chip that
 * happens to carry it — dropping {random} and {choice}, which the old
 * hand-written trigger strip always showed. This says explicitly which three
 * (and in which order) rather than bending VariableDef.pinned to fit a second
 * surface it was never meant to describe. */
const PINNED_OVERRIDE: Readonly<Record<string, readonly string[]>> = {
  triggers: ['user', 'random', 'choice']
};

/** The pinned chip row for one Surface, capped at six (VariablePalette's
 * fixed-height strip). A surface with an explicit override (see
 * PINNED_OVERRIDE) uses exactly that id list, in that order; everything else
 * falls back to VariableDef.pinned, and to the surface's own source order
 * when nothing on it is pinned at all (every reply/reward/builtin surface). */
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

/** Ids the two dedicated pickers (CounterPicker, FetchSourcePicker) already
 * own on the custom-command surface: offering them again as bare literal
 * chips in the "All variables" sheet would invite a broadcaster to ship a
 * `{counter:name}`/`{urlfetch:name}` that resolves to nothing (ResponseEditor's
 * old DEFAULT_TOKENS carried this same filter as `paletteTokens`). A timer has
 * neither picker, so its sheet keeps `{urlfetch:…}` — 'timer' is handled as
 * its own branch in sheetFor rather than through this set. */
const PICKER_OWNED_IDS = new Set(['counter', 'urlfetch']);

/** The manifest ids every reply chain resolves beyond its own token map
 * (rehearsal.ts's replyChain: PURE_SCOPE plus the reply's own tokens, nothing
 * else) that the sheet still offers as reference: {user} and {channel} are
 * substituted for every reply by modules/reply.go's Common() even though
 * neither is part of any one reply's own token list, and the rest is
 * scope.Pure exactly — the dice, the payload utilities with a literal-text
 * shape, and {if:…}. Grouped Who / Fun to match VariableGroup so they slot
 * into the sheet's ordinary group sections rather than needing a section of
 * their own. */
const REPLY_WHO_IDS = ['user', 'channel'];
const REPLY_FUN_IDS = ['random', 'choice', 'math', 'countdown', 'countup', 'repeat', 'if'];
const REPLY_MANIFEST_IDS = new Set([...REPLY_WHO_IDS, ...REPLY_FUN_IDS]);
// {channel}'s own VariableDef.group is 'stream' (the custom-command sheet
// sorts it under "Your stream", a fact about the broadcast): here it is
// re-grouped 'who', because on a reply surface it answers "whose chat is
// this" rather than "what is this stream doing" — a Who question, same as
// {user}. Every other id here already carries the group this constant wants
// (the seven REPLY_FUN_IDS are all 'fun' in VARIABLES; {user} is already
// 'who'), so this override is the one exception, not a pattern to extend.
const REPLY_MANIFEST_CHIPS: readonly VariableChip[] = VARIABLES.filter((v) => REPLY_MANIFEST_IDS.has(v.id))
  .flatMap(chipsOfVariable)
  .map((c) => (c.id === 'channel' ? { ...c, group: 'who' as const } : c));

/**
 * The "All variables" sheet's full content for one Surface — built from
 * chipsFor but not equal to it, for two independent reasons kept as two
 * branches rather than one combined filter:
 *
 *   - 'custom' drops the two picker-owned ids (see PICKER_OWNED_IDS): the
 *     chip ROW already has a dedicated picker for each, and a bare literal
 *     chip beside it would suggest a second, worse way to write the same
 *     token.
 *   - every reply-shaped surface ('triggers', a reward, or a {module,reply}/
 *     {builtin} pair) adds REPLY_MANIFEST_CHIPS on top of its own tokens:
 *     the chip row and "This reply" must stay exactly what that surface's
 *     rehearsal chain resolves, but the SHEET is reference material, and
 *     {user}/{channel}/the dice are real there even though they are not
 *     part of any one reply's OWN token list. Deduped by id so 'triggers'
 *     (whose own three chips are user/random/choice, already manifest
 *     Variables) does not show doubled rows for the ones REPLY_MANIFEST_CHIPS
 *     would otherwise add back.
 *
 * 'timer' returns chipsFor('timer') unchanged: it already includes
 * {urlfetch:…} (TIMER_SET; rehearsal.ts's EXTERNAL_SCOPE mounts it on
 * timerChain) and has no counter or fetch picker to defer to, so nothing
 * needs dropping or adding. */
export function sheetFor(surface: VariableSurface): readonly VariableChip[] {
  const own = chipsFor(surface);
  if (surface === 'custom') return own.filter((c) => !c.id || !PICKER_OWNED_IDS.has(c.id));
  if (surface === 'timer') return own;
  const ownIds = new Set(own.map((c) => c.id).filter((id): id is string => !!id));
  return [...own, ...REPLY_MANIFEST_CHIPS.filter((c) => !c.id || !ownIds.has(c.id))];
}
