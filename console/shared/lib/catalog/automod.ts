// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { ModuleDef } from './module-def';

export const AUTOMOD_MODULE: ModuleDef = 
{
  id: 'automod',
  // Beta: premium-only until it ships. Mirrors .Beta() on the sesame module
  // (app/twitch/sesame/modules/automod.go); both flip together.
  beta: true,
  label: 'AutoMod',
  tagline: 'Catch scams, IP-grabbers and raid spam before your mods do.',
  description:
    'The bot screens every chat line for harmful content and coordinated raid floods, and warns, deletes, times out or bans the sender. Trusted chatters (VIPs, mods, the broadcaster) are always exempt, and anything borderline is left to your human mods. Pick a level from None to All, then fine-tune each check below. The safety floor (hate slurs and IP-grabber links) is always enforced, on every level and even with the module off: hosting those risks your channel and the bot account platform-wide. Everything else is your call.',
  icon: 'moderation',
  category: 'Moderation',
  defaultEnabled: true,
  // AutoMod is pure configuration: no chat reply lines, only the settings strip.
  replies: [],
  settings: [
    {
      key: 'level',
      label: 'Enforcement level',
      type: 'select',
      placeholder: 'moderate',
      options: [
        { value: 'none', label: 'None - safety floor only' },
        { value: 'basic', label: 'Basic - floor + harassment' },
        { value: 'moderate', label: 'Moderate - recommended (default)' },
        { value: 'strict', label: 'All - every check, family-strict' }
      ],
      help: 'Sets the default for every check below. The safety floor applies at every level.'
    },
    {
      key: 'harassment',
      label: 'Harassment',
      type: 'toggle',
      followsLevel: true,
      help: 'Directed harm ("kys" and friends): warns the sender and removes the message; repeat offenders are timed out, then banned.'
    },
    {
      key: 'sexual',
      label: 'Sexual content',
      type: 'toggle',
      followsLevel: true,
      help: 'Removes messages with explicit sexual terms.'
    },
    {
      key: 'profanity',
      label: 'Profanity',
      type: 'toggle',
      followsLevel: true,
      help: 'Removes plain swearing. Off by default: most channels allow it.'
    },
    {
      key: 'style',
      label: 'Caps & symbol spam',
      type: 'toggle',
      followsLevel: true,
      help: 'Removes shouting, symbol walls and character floods. Emote walls (KEKW spam) are recognized and never flagged.'
    },
    {
      key: 'links',
      label: 'Link-spam radar',
      type: 'toggle',
      followsLevel: true,
      help: 'Watches for the same link template posted by many different accounts and removes the wave. Single links are never touched.'
    },
    {
      key: 'clips_only',
      label: 'Twitch clips only',
      type: 'toggle',
      help: 'Removes messages that contain any link other than a Twitch clip (clips.twitch.tv/… or twitch.tv/…/clip/…). Off by default: turn on when chat should only share clips.'
    },
    {
      key: 'block_terms',
      label: 'Blocked terms',
      type: 'textarea',
      placeholder: 'term one, term two',
      help: 'Extra words or phrases to flag in your channel. Separate with commas or new lines. Matched even through obfuscation (l33t, look-alike letters).'
    },
    {
      key: 'allow_terms',
      label: 'Allowed terms',
      type: 'textarea',
      placeholder: '',
      help: 'Words that are fine in your channel: a line containing one is never flagged by the checks above or your blocked terms. Cannot override the safety floor.'
    }
  ]
};
