// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { describe, expect, test } from 'bun:test';
import { MOD, MODULE_CATALOG, catalogIndexable, moduleDef, moduleDelegateSections } from './types';

type Def = NonNullable<ReturnType<typeof moduleDef>>;

const policyOf = (def: Def) => ({
  label: def.label,
  category: def.category,
  href: def.href,
  defaultEnabled: def.defaultEnabled,
  toggleable: def.toggleable !== false,
  listed: !def.hidden,
  delegates: moduleDelegateSections(def),
  settings: def.settings,
  replies: def.replies,
  commands: def.commands?.map(({ trigger, perm, aliases }) => ({ trigger, perm, aliases }))
});

interface PolicyRow {
  name: string;
  id: Parameters<typeof moduleDef>[0];
  want: Partial<ReturnType<typeof policyOf>>;
}

const POLICY_ROWS: PolicyRow[] = [
  {
    name: 'stream management is a Channel tile on /commands with no master switch',
    id: 'stream',
    want: { href: '/commands', toggleable: false, defaultEnabled: true, category: 'Channel', delegates: ['commands'] }
  },
  {
    name: 'stream management ships the Nightbot command set at lead_mod',
    id: 'stream',
    want: {
      commands: [
        { trigger: '!title', perm: 'lead_mod', aliases: ['!settitle'] },
        { trigger: '!game' },
        { trigger: '!tags' },
        { trigger: '!commercial' },
        { trigger: '!marker' },
        { trigger: '!cmd' }
      ]
    }
  },
  {
    name: 'folds counters into Modules delegation without an enable switch',
    id: 'counters',
    want: { href: '/counters', toggleable: false, delegates: ['modules'] }
  },
  {
    name: 'emoteplay is a toggle-only opt-in module keyed by its sesame name',
    id: 'emoteplay',
    want: { href: undefined, listed: true, toggleable: true, defaultEnabled: false, replies: [] }
  },
  {
    name: 'personality is a default-on Chat toggle with nothing to configure',
    id: 'personality',
    want: { category: 'Chat', defaultEnabled: true, toggleable: true, href: undefined, listed: true, replies: [], settings: undefined }
  },
  {
    name: 'personality owns !bagels and !bagelboard with their aliases',
    id: 'personality',
    want: {
      commands: [
        { trigger: '!bagels', aliases: ['!fed', '!bagelcount'], perm: undefined },
        { trigger: '!bagelboard', aliases: ['!feedboard', '!bagellb'], perm: undefined }
      ]
    }
  },
  {
    name: 'CODM is a generic Stats profile module with the shared account settings',
    id: 'codm',
    want: {
      label: 'CODM Profile',
      category: 'Stats',
      defaultEnabled: false,
      replies: [
        {
          key: 'profile',
          label: '!codm',
          command: 'codm',
          event: '!codm [UID/exact nickname]',
          enableKey: 'profileEnabled',
          messageKey: 'profileMessage',
          defaultMessage: '{codm:player} · level {codm:level} · MP {codm:rank} · {codm:rating} rating · {codm:country}',
          previewArgs: 'iFerg',
          tokens: [
            { name: 'codm:player', sample: 'iFerg' },
            { name: 'codm:level', sample: '414' },
            { name: 'codm:rank', sample: 'Master I' },
            { name: 'codm:rankclass', sample: '21' },
            { name: 'codm:rating', sample: '4590' },
            { name: 'codm:country', sample: 'US' },
            { name: 'codm:shortid', sample: 'IFERG' }
          ]
        }
      ],
      settings: [
        { key: 'account', help: 'Default profile for the command. If blank, enter a CODM UID or exact nickname after !codm.' },
        { key: 'linkedOnly' }
      ]
    }
  },
  { name: 'govee shares Gear with Song Requests and Discord', id: 'govee', want: { category: 'Gear' } },
  {
    name: 'songqueue shares Gear and is a bespoke href module listing !sr, !remove, !skip, !clear, !srlist and !current',
    id: 'songqueue',
    want: {
      category: 'Gear',
      href: '/songqueue',
      commands: [
        { trigger: '!sr' },
        { trigger: '!remove' },
        { trigger: '!skip' },
        { trigger: '!clear' },
        { trigger: '!srlist' },
        { trigger: '!current' }
      ]
    }
  },
  { name: 'songqueue opens for modules and channel-points delegates', id: 'songqueue', want: { delegates: ['modules', 'channelpoints'] } },
  { name: 'discord shares Gear and delegates on its own grant, not modules', id: 'discord', want: { category: 'Gear', href: '/discord', delegates: ['discord'] } },
  { name: 'AutoMod stays a visible Moderation row', id: 'automod', want: { listed: true, category: 'Moderation' } },
  { name: 'loyalty owns the /loyalty page', id: 'loyalty', want: { href: '/loyalty' } }
];

describe('module catalog', () => {
  test('has unique ids', () => {
    const ids = MODULE_CATALOG.map((def) => def.id);
    expect(new Set(ids).size).toBe(ids.length);
  });

  test('every MOD entry names a real catalog module', () => {
    for (const [key, value] of Object.entries(MOD)) {
      expect(key).toBe(value);
      expect(moduleDef(value)).toBeDefined();
    }
  });

  test('every catalog module has a MOD key', () => {
    const catalogIds = MODULE_CATALOG.map((def) => def.id).sort();
    const modValues = new Set(Object.values(MOD));
    const missing = catalogIds.filter((id) => !modValues.has(id));
    expect(missing).toEqual([]);
  });

  test.each(POLICY_ROWS)('$name', ({ id, want }) => {
    expect(policyOf(moduleDef(id)!)).toMatchObject(want);
  });

  test('a sectioned module is kept out of the modules grid', () => {
    const discord = moduleDef('discord')!;
    expect(discord.section).toBe(true);
    expect(catalogIndexable(discord)).toBe(false);
    expect(discord.hidden).toBeUndefined();
  });

  test('every other listed module still indexes', () => {
    const listed = MODULE_CATALOG.filter((d) => !d.hidden && !d.parent && !d.section);
    expect(listed.length).toBeGreaterThan(0);
    expect(listed.every(catalogIndexable)).toBe(true);
  });

  test('gamble and duels nest under loyalty with no second currency name', () => {
    for (const id of ['gamble', 'duel'] as const) {
      const def = moduleDef(id);
      expect(def?.parent).toBe('loyalty');
      expect(def?.href).toBeUndefined();
      expect(def?.settings?.some((s) => s.key === 'pointsName')).toBe(false);
    }
  });
});
