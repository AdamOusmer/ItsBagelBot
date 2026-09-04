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
  // aliases are extra chat triggers that resolve to this built-in (e.g.
  // settitle → title). Shown on the commands page next to the primary name.
  aliases?: string[];
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
    // app/twitch/outgress/internal/worker clipReplyText). {user} = the clipper, {target}
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
    // read by sesame and expanded by outgress (see app/twitch/sesame/modules/clip.go).
    editable: true,
    replyKey: 'reply',
    tokens: ['clip', 'user', 'target']
  },
  {
    id: 'title',
    label: 'Title',
    aliases: ['settitle'],
    summary: 'Built-in · show or set the stream title.',
    description:
      'Lead moderators read the current title with !title, or set a new one with !title <title> / !settitle <title>. Empty !settitle prints usage instead of reading. Max 140 characters.',
    usage: ['!title', '!title <title>', '!settitle <title>'],
    preview: '@{user} updated the title to: {title}',
    previewArgs: 'Ranked grind',
    previewSamples: { user: 'lead_mod', title: 'Ranked grind' },
    defaultActive: true,
    defaultPerm: 'lead_mod',
    defaultCooldown: 5,
    liveOnly: false
  },
  {
    id: 'game',
    label: 'Game',
    aliases: ['setgame'],
    summary: 'Built-in · show or set the stream category.',
    description:
      'Lead moderators read the current category with !game, or set it with !game <name> / !setgame <name>. The bot searches Twitch categories and applies the first match. Empty !setgame prints usage.',
    usage: ['!game', '!game <name>', '!setgame <name>'],
    preview: '@{user} updated the game to: {game}',
    previewArgs: 'Fortnite',
    previewSamples: { user: 'lead_mod', game: 'Fortnite' },
    defaultActive: true,
    defaultPerm: 'lead_mod',
    defaultCooldown: 5,
    liveOnly: false
  },
  {
    id: 'tags',
    label: 'Tags',
    aliases: ['settags'],
    summary: 'Built-in · show or set stream tags.',
    description:
      'Lead moderators read the current tags with !tags, or replace them with a comma-separated list (!tags just chatting, english). At most 10 tags, 25 characters each. Empty !settags prints usage.',
    usage: ['!tags', '!tags <tag1, tag2>', '!settags <tag1, tag2>'],
    preview: '@{user} updated tags to: {tags}',
    previewArgs: 'English, family friendly',
    previewSamples: { user: 'lead_mod', tags: 'English, family friendly' },
    defaultActive: true,
    defaultPerm: 'lead_mod',
    defaultCooldown: 5,
    liveOnly: false
  },
  {
    id: 'commercial',
    label: 'Commercial',
    aliases: ['ad'],
    summary: 'Built-in · run a mid-roll commercial on the live stream.',
    description:
      'Lead moderators start a Twitch mid-roll while you are live. Bare !commercial runs 30 seconds; otherwise pick 30, 60, 90, 120, 150 or 180. Needs the channel:edit:commercial grant.',
    usage: ['!commercial', '!commercial 60', '!ad 90'],
    preview: '@{user} started a {length}s commercial.',
    previewArgs: '60',
    previewSamples: { user: 'lead_mod', length: '60' },
    defaultActive: true,
    defaultPerm: 'lead_mod',
    defaultCooldown: 30,
    liveOnly: true
  },
  {
    id: 'marker',
    label: 'Marker',
    summary: 'Built-in · drop a stream marker on the live broadcast.',
    description:
      'Lead moderators drop a Twitch stream marker while you are live, with an optional description (max 140 characters). Needs the channel:manage:broadcast grant.',
    usage: ['!marker', '!marker <description>'],
    preview: '@{user} dropped a stream marker.',
    previewArgs: 'Boss fight',
    previewSamples: { user: 'lead_mod' },
    defaultActive: true,
    defaultPerm: 'lead_mod',
    defaultCooldown: 10,
    liveOnly: true
  }
];

export function builtinDef(id: string): BuiltinCommandDef | undefined {
  return BUILTIN_COMMANDS.find((b) => b.id === id);
}

export const BUILTIN_NAMES: ReadonlySet<string> = new Set(BUILTIN_COMMANDS.map((b) => b.id));
