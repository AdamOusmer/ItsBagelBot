// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { IconName } from '../icons';

// --- Module catalog -------------------------------------------------------
// The user-facing modules the dashboard lets a broadcaster toggle/configure.
// Core, hidden modules (the command processor, the live tracker, and the system
// module that owns !ping/!itsbagelbot and the bagel greeting) are deliberately
// NOT listed here: they are always on and never shown.

export type ModuleFieldType = 'text' | 'textarea' | 'number' | 'select' | 'toggle' | 'timezone';

export interface ModuleField {
  // key is the JSON property written into the module's Configs blob.
  key: string;
  label: string;
  type: ModuleFieldType;
  placeholder?: string;
  help?: string;
  // options drive a 'select' field.
  options?: { value: string; label: string }[];
  // followsLevel marks a 'toggle' whose unset state follows the module's
  // "level" select (see automodToggleDefault): the blob only stores an
  // explicit "on"/"off" once the user flips it.
  followsLevel?: boolean;
}

// Mirrors levelSections in app/sesame/automod/config.go: which automod sections
// each level preset enables. Renders the resting state of a follows-level
// toggle; the authoritative resolution happens in Go.
export const AUTOMOD_LEVEL_DEFAULTS: Record<string, Record<string, boolean>> = {
  none: { harassment: false, sexual: false, profanity: false, style: false, links: false },
  basic: { harassment: true, sexual: false, profanity: false, style: false, links: false },
  moderate: { harassment: true, sexual: true, profanity: false, style: true, links: true },
  strict: { harassment: true, sexual: true, profanity: true, style: true, links: true }
};

// automodToggleDefault resolves a follows-level toggle's resting state for the
// currently selected level.
export function automodToggleDefault(level: string, key: string): boolean {
  return (AUTOMOD_LEVEL_DEFAULTS[level] ?? AUTOMOD_LEVEL_DEFAULTS.moderate)[key] ?? false;
}

// One chat line a module can post, rendered as a row on the module page. Clicking
// the row opens the exact same builder as a custom command's response (the shared
// ResponseEditor + ChatPreview, standard {user}/{target}/… tokens). messageKey/
// enableKey are the Configs JSON keys the matching sesame module reads (see
// app/sesame/modules).
export interface ModuleReply {
  key: string; // stable row id
  label: string; // 'Follow alert'
  tagline: string; // short row description
  // Preview context: what fires this line, e.g. 'on follow'.
  event: string;
  messageKey: string; // Configs key holding the template
  // Configs key for this reply's own on/off toggle; omit when the reply has no
  // per-reply switch (it fires whenever the module is on). Stored "on"/"off";
  // empty/absent means on, matching sesame's alertOn semantics.
  enableKey?: string;
  // Flips the empty/absent default to OFF: the reply fires only on an explicit
  // "on" (sesame's adAlertOn semantics). Used by alerts the broadcaster must
  // opt into, like the ad-break announcement.
  defaultOff?: boolean;
  defaultMessage: string; // sesame default template (placeholder + preview fallback)

  // --- command-style replies (gossip modules: urchin, mcsr) ---------------
  // command is the chat trigger without '!' (e.g. 'daily'). When set, the
  // inspector rehearses the reply exactly like a custom command: the border
  // reads "Chat rehearsal", a viewer line types the trigger, and the bot
  // answers with previewSamples substituted into the template.
  command?: string;
  // previewArgs is what the sample viewer types after the trigger.
  previewArgs?: string;
  // previewSamples maps this reply's tokens to sample values. The rehearsal
  // (kind="reply") substitutes ONLY these plus the shared dynamic tokens:
  // sesame expands only the module's own token map here, so the generic
  // command samples would preview values the bot will never produce.
  previewSamples?: Record<string, string>;
  // tokens is the editor's insert palette (without braces), replacing the
  // default command tokens with the ones this reply actually supports.
  tokens?: string[];
}

// A chat command a module exposes, listed read-only on the module page so a
// broadcaster can see what turning the module on unlocks. Unlike a ModuleReply
// these are not editable or toggleable: modules whose replies are fixed system
// lines (the queue) have nothing to configure per command, so the rows are
// informational only — never clickable.
export interface ModuleCommandInfo {
  trigger: string; // the chat trigger with '!' (e.g. '!join', '!queue next')
  summary: string; // one-line description of what it does
  // perm names the minimum role, shown as a small tag. Omit for everyone.
  perm?: 'mod' | 'lead_mod';
  aliases?: string[];
}

export interface ModuleDef {
  // id is normally the ModuleView.name key in the modules service. Catalog
  // tools with toggleable=false use it only as their stable UI key.
  id: string;
  label: string;
  tagline: string; // one-liner for the tile
  description: string; // longer copy for the module page
  icon: IconName;
  category: string;
  defaultEnabled: boolean;
  // False for catalog tools that live under Modules but are always available
  // rather than backed by an enableable row in the modules service. They keep
  // the shared tile/delegation model without presenting a meaningless switch.
  toggleable?: boolean;
  // If true, the module is hidden from the dashboard and unreachable.
  hidden?: boolean;
  // If true, the module is in beta: listed to everyone with a Beta chip, but
  // premium-only (paid/vip) until the flag is removed. Free channels see the
  // tile locked, cannot toggle or write it, and its bespoke page (href)
  // bounces to /modules from the route guard. Mirrors the sesame side, which
  // skips a Beta module on the standard lane (app/sesame/module, Module.Beta);
  // both flags flip in the same PR since Go and TS share no catalog. Ending a
  // beta is deleting both flags: rows a channel enabled while premium stay and
  // simply resume, no data migration.
  beta?: boolean;
  // The module's configurable chat lines (the "commands" of the module page).
  replies: ModuleReply[];
  // Read-only chat commands to list on the module page. For modules that expose
  // commands with fixed (non-customizable) replies, e.g. the play queue. Shown
  // as static rows, never clickable. Optional.
  commands?: ModuleCommandInfo[];
  // Plain non-reply settings (rendered in the settings strip). Optional; the
  // current modules have none beyond their master enable + per-reply toggles.
  settings?: ModuleField[];
  // parent is the catalog id this module nests under. Nested modules do not
  // get their own index tile or an independent master switch: they appear as
  // compact rows on the parent's page, because they cannot run unless the
  // parent is on (gamble/duel spend the loyalty ledger). The generic
  // /modules/[id] inspector still configures their unique knobs and replies.
  parent?: string;
  // href overrides the tile's link when a module needs a bespoke inspector
  // instead of the generic /modules/[id] reply page (govee's device + reward
  // setup). Absent for the ordinary reply-configured modules.
  href?: string;
  // delegateSections names the delegation grants that open the bespoke href
  // page for a delegate. Defaults to ['modules'] (the tile lives on /modules),
  // so it only needs setting when a page has its own grant (channel points) or
  // additionally rides another one (timers/quotes under 'commands'). Read via
  // moduleDelegateSections; the route guard, the per-page gates and the tile
  // grid all derive from it, so a new bespoke page never touches those.
  delegateSections?: readonly string[];
}

// moduleDelegateSections resolves a def's delegation scope (see the field doc).
export function moduleDelegateSections(def: ModuleDef): readonly string[] {
  return def.delegateSections ?? ['modules'];
}

// A module's current state as shown on the dashboard: catalog metadata merged
// with the broadcaster's stored row.
export interface ModuleState {
  def: ModuleDef;
  enabled: boolean;
  config: Record<string, string>;
  // locked is set by the dashboard's server load when the module is in beta
  // and the broadcaster is not premium (betaLocked). The tile then shows the
  // lock in place of its switch. Absent means open.
  locked?: boolean;
}

// betaLocked reports whether a module is closed to this broadcaster: in beta
// and the channel is not premium. The one rule both the tile grid and the
// write gates apply, so a locked tile can never be toggled through a stale
// form.
export function betaLocked(def: ModuleDef, premium: boolean): boolean {
  return def.beta === true && !premium;
}
