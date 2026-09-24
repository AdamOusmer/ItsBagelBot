// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { Perm } from '../types';
import { replyTokens, type ReplyToken } from './module-def';

export interface BuiltinCommandDef {
  id: string;
  label: string;
  summary: string;
  description: string;
  usage: string[];
  preview: string;
  previewArgs?: string;
  defaultActive: boolean;
  defaultPerm: Perm;
  defaultCooldown: number;
  liveOnly: boolean;
  editable?: boolean;
  replyKey?: string;
  tokens?: readonly ReplyToken[];
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
    tokens: replyTokens(['target'], { target: 'viewer' }),
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
    tokens: replyTokens(['target'], { target: 'viewer' }),
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
    preview: '{user} clipped: {target} → {clip}',
    previewArgs: 'That is amazing',
    defaultActive: true,
    defaultPerm: 'everyone',
    defaultCooldown: 15,
    liveOnly: true,
    editable: true,
    replyKey: 'reply',
    tokens: replyTokens(['clip', 'user', 'target'], { user: 'sesame_sam', target: 'That is amazing', clip: 'clips.twitch.tv/AbCdEf' }, 'builtin.clip')
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
    tokens: replyTokens(['user', 'title'], { user: 'lead_mod', title: 'Ranked grind' }),
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
    tokens: replyTokens(['user', 'game'], { user: 'lead_mod', game: 'Fortnite' }),
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
    tokens: replyTokens(['user', 'tags'], { user: 'lead_mod', tags: 'English, family friendly' }),
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
    tokens: replyTokens(['user', 'length'], { user: 'lead_mod', length: '60' }),
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
    tokens: replyTokens(['user'], { user: 'lead_mod' }),
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
