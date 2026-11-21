// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { describe, expect, test } from 'bun:test';
import { staticText } from '../../../../kit/lib/i18n/static';
import { stubSvelteKit } from '../../../test/sveltekit';

stubSvelteKit();

const { publicCommands, publicModules } = await import('./public-directory');
const { MODULE_CATALOG } = await import('@bagel/kit/catalog');

describe('public directory locale shaping', () => {
  test('localizes catalog module metadata and command summaries', () => {
    const shaped = publicModules([{ name: 'queue', is_enabled: true }], 'fr').find((entry) => entry.id === 'queue');
    expect({ tagline: shaped?.tagline, firstCommand: shaped?.commands[0].meta }).toEqual({
      tagline: staticText('fr', 'modules.catalog.queue.tagline'),
      firstCommand: staticText('fr', 'modules.catalog.queue.commands.join')
    });
    expect(shaped?.tagline).not.toBe(staticText('en', 'modules.catalog.queue.tagline'));
  });

  test('localizes finite permission labels while preserving custom response text and both cooldowns', () => {
    const [row] = publicCommands(
      [{ name: 'hello', aliases: [], response: 'Welcome to the channel!', perm: 'sub', cooldown: 5, user_cooldown: 60, stream_online_only: false, uses: '0', is_active: true }],
      'fr'
    );
    expect({ perm: row.perm, response: row.response, cooldowns: [row.cooldown, row.userCooldown] }).toEqual({
      perm: staticText('fr', 'perm.sub'),
      response: 'Welcome to the channel!',
      cooldowns: [5, 60]
    });
  });
});

describe('public directory module commands', () => {
  const commandsOf = (rows: Parameters<typeof publicModules>[0], id: string) =>
    publicModules(rows, 'en').find((entry) => entry.id === id)?.commands ?? [];

  test('Bedwars tag names stay current in the dashboard public directory', () => {
    for (const locale of ['en', 'fr'] as const) {
      const entry = publicModules([{ name: 'urchin', is_enabled: true }], locale).find((entry) => entry.id === 'urchin')!;
      const tagCmd = entry.commands.find((command) => command.label === '!bwtag');
      const labels = entry.commands.map((command) => command.label);
      expect({
        label: tagCmd?.label,
        aliases: tagCmd?.aliases,
        legacy: labels.filter((label) => label === '!tag' || label === '!tags')
      }).toEqual({
        label: '!bwtag',
        aliases: ['!bwtags'],
        legacy: []
      });
    }
    const disabled = commandsOf([{ name: 'urchin', is_enabled: true, configs: { tagsEnabled: 'off' } }], 'urchin');
    expect(disabled.some((command) => command.label.includes('!bwtag'))).toBe(false);
  });

  const disabledTriggerCases: { name: string; id: string; disabledTriggers: string; want: { label: string; aliases: string[] }[] }[] = [
    {
      name: 'disabling one alias drops only that alias',
      id: 'quotes',
      disabledTriggers: 'quotes',
      want: [
        { label: '!quote', aliases: [] },
        { label: '!quote <number>', aliases: [] },
        { label: '!quote <word>', aliases: [] },
        { label: '!addquote <text>', aliases: ['!quoteadd'] },
        { label: '!quote edit <number> <text>', aliases: [] },
        { label: '!quote remove <number>', aliases: [] }
      ]
    },
    {
      name: 'disabling the primary trigger promotes the first remaining alias and keeps arguments',
      id: 'quotes',
      disabledTriggers: 'addquote',
      want: [
        { label: '!quote', aliases: ['!quotes'] },
        { label: '!quote <number>', aliases: [] },
        { label: '!quote <word>', aliases: [] },
        { label: '!quoteadd <text>', aliases: [] },
        { label: '!quote edit <number> <text>', aliases: [] },
        { label: '!quote remove <number>', aliases: [] }
      ]
    },
    {
      name: 'disabling all triggers of a command hides it and its subcommand rows',
      id: 'quotes',
      disabledTriggers: 'quote,quotes',
      want: [{ label: '!addquote <text>', aliases: ['!quoteadd'] }]
    },
    {
      name: 'disabling a Bedwars primary trigger promotes its alias without re-adding the reply fallback',
      id: 'urchin',
      disabledTriggers: 'bwtag',
      want: [
        { label: '!bw', aliases: ['!bedwars'] },
        { label: '!daily', aliases: ['!bwdaily'] },
        { label: '!weekly', aliases: ['!bwweekly'] },
        { label: '!monthly', aliases: ['!bwmonthly'] },
        { label: '!bwstats', aliases: [] },
        { label: '!sniper', aliases: ['!urchin'] },
        { label: '!bwtags', aliases: [] },
        { label: '!tagdescription', aliases: [] }
      ]
    }
  ];

  test.each(disabledTriggerCases)('disabledTriggers: $name', ({ id, disabledTriggers, want }) => {
    const got = commandsOf([{ name: id, is_enabled: true, configs: { disabledTriggers } }], id).map(
      ({ label, aliases }) => ({ label, aliases })
    );
    expect(got).toEqual(want);
  });

  test('lists each trigger once, not once per reply line', () => {
    const raffle = commandsOf([{ name: 'raffle', is_enabled: true }], 'raffle').map((c) => c.label);
    expect(raffle.filter((label) => label === '!join')).toHaveLength(1);
    expect(new Set(raffle).size).toBe(raffle.length);

    const queue = commandsOf([{ name: 'queue', is_enabled: true }], 'queue').map((c) => c.label);
    expect(queue.filter((label) => label === '!join')).toHaveLength(1);
    expect(queue).toEqual(MODULE_CATALOG.find((def) => def.id === 'queue')!.commands!.map((c) => c.trigger));
  });

  test('lists !claim once under enabled giveaways', () => {
    const raffle = { name: 'raffle', is_enabled: true, configs: { claimEnabled: 'on' } };
    const rows = [raffle, { name: 'giveaway', is_enabled: true }];
    expect(commandsOf(rows, 'raffle').some((command) => command.label === '!claim')).toBe(false);
    expect(commandsOf(rows, 'giveaway').filter((command) => command.label === '!claim')).toHaveLength(1);
    expect(commandsOf([raffle], 'giveaway')).toHaveLength(0);
  });

  test('raffle !join reads as a command, not a reply line', () => {
    const join = commandsOf([{ name: 'raffle', is_enabled: true }], 'raffle').find((c) => c.label === '!join');
    expect(join?.meta).toBe(staticText('en', 'modules.catalog.raffle.commands.join'));
  });

  test('shows the level each command needs', () => {
    const queue = commandsOf([{ name: 'queue', is_enabled: true }], 'queue');
    expect(queue.find((c) => c.label === '!join')?.perm).toBe(staticText('en', 'perm.everyone'));
    expect(queue.find((c) => c.label === '!queue next')?.perm).toBe(staticText('en', 'perm.mod'));
  });

  test('honours a saved level for a command that has one', () => {
    const cmd = commandsOf([{ name: 'cmd', is_enabled: true, configs: { addPerm: 'vip' } }], 'cmd');
    expect(cmd.find((c) => c.label.startsWith('!cmd add'))?.perm).toBe(staticText('en', 'perm.vip'));
  });

  test('built-ins carry their saved level and summary', () => {
    const [row] = commandsOf([{ name: 'accountage', is_enabled: true, configs: { permission: 'sub' } }], 'accountage');
    expect({ label: row.label, meta: row.meta, perm: row.perm }).toEqual({
      label: '!accountage',
      meta: staticText('en', 'builtinDirectory.accountage.summary'),
      perm: staticText('en', 'perm.sub')
    });
  });
});

test('public commands retain every digit of a maximum int64 use count', () => {
  const [command] = publicCommands([{ name: 'hello', response: 'Hello', is_active: true, uses: '9223372036854775807' }], 'en');
  expect(command.uses).toBe('9,223,372,036,854,775,807');
});
