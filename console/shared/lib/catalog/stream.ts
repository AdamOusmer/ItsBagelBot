// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { ModuleDef } from './module-def';

// Stream Management is KindCore in sesame (!cmd stays always-on) plus
// per-command toggles for !title/!game/!tags/!commercial/!marker. The tile
// has no master switch — a missing row is on — and the commands grant
// already opens /commands, so this is discovery + the same grant, not a
// second permission.
export const STREAM_MODULE: ModuleDef = {
  id: 'stream',
  label: 'Stream Management',
  tagline: 'Set the live title, category and tags, run ads, and drop markers from chat.',
  description:
    'Lead moderators edit the live stream from chat the way Nightbot and StreamElements do: !title / !settitle, !game / !setgame, !tags, !commercial / !ad, and !marker. Each command can be toggled on this page. They ship on. Needs a Twitch re-consent for channel:manage:broadcast and channel:edit:commercial if you signed up before those grants existed.',
  icon: 'broadcast',
  category: 'Channel',
  defaultEnabled: true,
  toggleable: false,
  href: '/commands',
  delegateSections: ['commands'],
  replies: [],
  commands: [
    { trigger: '!title', aliases: ['!settitle'], summary: 'Show or set the stream title.', perm: 'lead_mod' },
    { trigger: '!game', aliases: ['!setgame'], summary: 'Show or set the stream category.', perm: 'lead_mod' },
    { trigger: '!tags', aliases: ['!settags'], summary: 'Show or set stream tags.', perm: 'lead_mod' },
    { trigger: '!commercial', aliases: ['!ad'], summary: 'Run a mid-roll commercial (live only).', perm: 'lead_mod' },
    { trigger: '!marker', summary: 'Drop a stream marker (live only).', perm: 'lead_mod' },
    { trigger: '!cmd', aliases: ['!cmds', '!command', '!commands'], summary: 'List commands, or add/edit/remove custom ones.', perm: 'mod' }
  ]
};
