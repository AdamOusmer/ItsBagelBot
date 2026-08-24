// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { Perm } from '../types';

// --- Built-in command catalog --------------------------------------------
// Built-in commands are behaviors baked into the bot (not user text). They show
// on the commands page alongside custom commands, flagged builtin, but they
// cannot be renamed, deleted, or given a custom response — only toggled on/off.
// Their per-user on/off state lives in the modules service under `id` (a missing
// row means defaultActive). Adding one is a row here + the matching sesame
// built-in module. They are deliberately NOT in MODULE_CATALOG (never shown on
// the modules page).

export interface BuiltinCommandDef {
  // id is both the chat trigger and the modules-service key for the toggle.
  id: string;
  label: string;
  // summary is shown in the command row where a custom command shows its
  // response (built-ins have no response).
  summary: string;
  description: string; // longer copy for the inspector
  // usage lists example invocations shown in the inspector.
  usage: string[];
  // preview is the bot REPLY template, rendered through ChatPreview (as a
  // reply rehearsal: only previewSamples substitute — built-in replies are
  // bare token replacers with no dynamic tokens or slash-verb routing).
  // previewArgs is what the viewer types after the trigger.
  preview: string;
  previewArgs?: string;
  previewSamples?: Record<string, string>;
  defaultActive: boolean;
  defaultPerm: Perm;
  defaultCooldown: number; // seconds
  // liveOnly commands run only while the broadcaster is streaming.
  liveOnly: boolean;
  // editable: the reply template can be customized on the dashboard. When true
  // the inspector shows a ResponseEditor (with the `tokens` palette) and a
  // rehearsal, and saves the template into the modules-service config under
  // `replyKey`. The bot expands the tokens when it posts the reply (e.g. {clip}
  // → the clip URL, resolved by outgress once the clip exists). Non-editable
  // built-ins stay a read-only preview. `preview` doubles as the default
  // template when no custom reply is set.
  editable?: boolean;
  // replyKey is the Configs key the custom reply template is stored under (only
  // meaningful when editable).
  replyKey?: string;
  // tokens is the reply editor's insert palette (token names without braces).
  tokens?: string[];
}

export const BUILTIN_COMMANDS: readonly BuiltinCommandDef[] = [
  {
    id: 'accountage',
    label: 'Account age',
    summary: 'Built-in · shows how long a Twitch account has existed.',
    description:
      'Shows how old your Twitch account is, or the age of another Twitch user\'s account when you add their username.',
    usage: ['!accountage', '!accountage <user>'],
    preview: "@{target}'s account is 4 years, 2 months old.",
    previewArgs: 'viewer',
    previewSamples: { target: 'viewer' },
    defaultActive: true,
    defaultPerm: 'everyone',
    defaultCooldown: 15,
    liveOnly: false
  },
  {
    id: 'followage',
    label: 'Followage',
    summary: 'Built-in · shows how long a viewer has followed the channel.',
    description:
      'Shows how long you or another Twitch user has followed the channel. Add a username to look up someone else.',
    usage: ['!followage', '!followage <user>'],
    preview: '@{target} has followed for 8 months.',
    previewArgs: 'viewer',
    previewSamples: { target: 'viewer' },
    defaultActive: true,
    defaultPerm: 'everyone',
    defaultCooldown: 15,
    liveOnly: false
  },
  {
    id: 'uptime',
    label: 'Uptime',
    summary: 'Built-in · shows how long the current stream has been live.',
    description:
      'Shows how long your current stream has been running. Replies that you are offline when no stream is up.',
    usage: ['!uptime'],
    preview: 'The stream has been live for 2 hours, 5 minutes.',
    defaultActive: true,
    defaultPerm: 'everyone',
    defaultCooldown: 15,
    liveOnly: false
  },
  {
    id: 'clip',
    label: 'Clip',
    summary: 'Built-in · clips the last moments of the stream and posts the link.',
    description:
      'Viewers create a clip of the recent stream and the bot replies in chat with the clip link. Add an optional title after the command. Only works while you are live.',
    usage: ['!clip', '!clip <title>'],
    // Real reply format: "<clipper> clipped: <title> → <url>" (see
    // app/outgress/internal/worker clipReplyText). {user} = the clipper, {target}
    // = the title argument (standard command token).
    preview: '{user} clipped: {target} → {clip}',
    previewArgs: 'That is amazing',
    previewSamples: { user: 'sesame_sam', target: 'That is amazing', clip: 'clips.twitch.tv/AbCdEf' },
    defaultActive: true,
    defaultPerm: 'everyone',
    defaultCooldown: 15,
    liveOnly: true,
    // The reply is customizable: {clip} is the clip link, {user} the clipper,
    // {target} the title the viewer typed. Stored under the "reply" config key,
    // read by sesame and expanded by outgress (see app/sesame/modules/clip.go).
    editable: true,
    replyKey: 'reply',
    tokens: ['clip', 'user', 'target']
  }
];

export function builtinDef(id: string): BuiltinCommandDef | undefined {
  return BUILTIN_COMMANDS.find((b) => b.id === id);
}

export const BUILTIN_NAMES: ReadonlySet<string> = new Set(BUILTIN_COMMANDS.map((b) => b.id));
