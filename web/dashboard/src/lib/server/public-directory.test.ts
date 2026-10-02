// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { describe, expect, test } from 'bun:test';
import { staticText } from '../../../../kit/lib/i18n/static';
import { stubSvelteKit } from '../../../test/sveltekit';

stubSvelteKit();

const { publicCommands, publicModules } = await import('./public-directory');
const { MODULE_CATALOG } = await import('@bagel/kit/catalog');

describe('public directory locale shaping', () => {
  test('localizes catalog module metadata and reply event copy', () => {
    const queue = MODULE_CATALOG.find((def) => def.id === 'queue')!;
    const joinKey = `modules.catalog.queue.replies.${queue.replies[0].key}.tagline`;
    const shaped = publicModules([{ name: 'queue', is_enabled: true }], 'fr').find((entry) => entry.id === 'queue');
    expect({ tagline: shaped?.tagline, firstCommand: shaped?.commands[0].meta }).toEqual({
      tagline: staticText('fr', 'modules.catalog.queue.tagline'),
      firstCommand: staticText('fr', joinKey)
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

test('public commands retain every digit of a maximum int64 use count', () => {
  const [command] = publicCommands([{ name: 'hello', response: 'Hello', is_active: true, uses: '9223372036854775807' }], 'en');
  expect(command.uses).toBe('9,223,372,036,854,775,807');
});
