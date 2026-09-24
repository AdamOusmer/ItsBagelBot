// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { dev } from '$app/environment';
import type {
  CommitResponse,
  ImportDiagnostic,
  ImportManifest,
  ImportSource,
  PreviewResponse
} from '@bagel/kit';

if (!dev) throw new Error('DASHBOARD_DEV_FIXTURE_INCLUDED_IN_PRODUCTION');

function demoManifest(): ImportManifest {
  return {
    commands: demoCommands(),
    timers: demoTimers(),
    triggers: demoTriggers(),
    quotes: demoQuotes()
  };
}

function demoCommands(): NonNullable<ImportManifest['commands']> {
  return [
    {
      name: 'discord',
      aliases: ['dc', 'social'],
      responses: ['Come hang out: discord.gg/bagelfam'],
      permission: 'everyone',
      cooldown_seconds: 10
    },
    {
      name: 'socials',
      aliases: ['twitter'],
      responses: ['Follow everywhere: twitter.com/itsmavey - bsky.app/itsmavey'],
      permission: 'everyone',
      cooldown_seconds: 30
    },
    {
      name: 'lurk',
      responses: ['{user} slips into the shadows. Enjoy the lurk!'],
      permission: 'everyone',
      cooldown_seconds: 5
    },
    {
      name: 'followage',
      responses: ['{user}, thanks for following the channel!'],
      permission: 'everyone',
      cooldown_seconds: 15,
      warnings: [
        'Source variable $(user.followage) has no BagelBot equivalent yet, so it was replaced with static text.'
      ]
    },
    {
      name: 'deaths',
      responses: ['{target} has died {counter:deaths} times. Skill issue.'],
      permission: 'everyone',
      cooldown_seconds: 5
    },
    {
      name: 'so',
      aliases: ['shoutout'],
      responses: ['Go check out {target} at twitch.tv/{target}, worth the follow!'],
      permission: 'mod',
      cooldown_seconds: 15
    }
  ];
}

function demoTimers(): NonNullable<ImportManifest['timers']> {
  return [
    {
      message: 'Enjoying the stream? Hit follow so you catch the next raid!',
      interval_seconds: 900,
      online_only: true
    },
    {
      message: 'Lurkers welcome. Loyalty points tick up while you watch.',
      interval_seconds: 1200,
      online_only: true
    }
  ];
}

function demoTriggers(): NonNullable<ImportManifest['triggers']> {
  return [
    { phrase: 'good vibes', response: 'Right back at you!' },
    { phrase: 'waffle', response: 'The bagel defends the waffle. Barely.' }
  ];
}

function demoQuotes(): NonNullable<ImportManifest['quotes']> {
  return [
    { text: 'Is a hotdog a sandwich? Asking for chat.', added_by: 'Mavey' },
    { text: 'I did not die, I respawned tactically.', added_by: 'pixlpuff' },
    { text: 'This boss has two phases: me winning, then not.', added_by: 'Mavey' }
  ];
}

function stats(m: ImportManifest) {
  return {
    commands: m.commands?.length ?? 0,
    timers: m.timers?.length ?? 0,
    triggers: m.triggers?.length ?? 0,
    quotes: m.quotes?.length ?? 0
  };
}

export function demoImportPreview(_source: ImportSource): PreviewResponse {
  const manifest = demoManifest();
  const diagnostics: ImportDiagnostic[] = [
    {
      severity: 'warn',
      item_index: 3,
      code: 'command_variable_unmapped',
      message: 'followage: source variable had no BagelBot equivalent, response rewritten around it.'
    },
    {
      severity: 'warn',
      item_index: 0,
      code: 'timer_online_only_widened',
      message: 'Source marked this timer offline-capable; BagelBot timers run while live only.'
    }
  ];
  return {
    manifest,
    diagnostics,
    collisions: [{ kind: 'command', name: 'discord' }],
    stats: stats(manifest)
  };
}

export function demoImportCommit(manifest: ImportManifest, overwrite: boolean): CommitResponse {
  const applied = stats(manifest);
  let skipped: CommitResponse['skipped'];
  if (!overwrite && manifest.commands?.some((c) => c.name === 'discord')) {
    manifest = {
      ...manifest,
      commands: manifest.commands.filter((c) => c.name !== 'discord')
    };
    applied.commands -= 1;
    skipped = [{ kind: 'command', name: 'discord' }];
  }
  return { applied, skipped, audit_id: 4311 };
}
