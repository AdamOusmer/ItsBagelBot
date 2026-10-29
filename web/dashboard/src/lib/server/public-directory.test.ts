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

  test('lists each trigger once, not once per reply line', () => {
    const raffle = commandsOf([{ name: 'raffle', is_enabled: true }], 'raffle').map((c) => c.label);
    expect(raffle.filter((label) => label === '!join')).toHaveLength(1);
    expect(new Set(raffle).size).toBe(raffle.length);

    const queue = commandsOf([{ name: 'queue', is_enabled: true }], 'queue').map((c) => c.label);
    expect(queue.filter((label) => label === '!join')).toHaveLength(1);
    expect(queue).toEqual(MODULE_CATALOG.find((def) => def.id === 'queue')!.commands!.map((c) => c.trigger));
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
