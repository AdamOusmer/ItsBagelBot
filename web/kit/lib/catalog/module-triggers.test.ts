// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { describe, expect, test } from 'bun:test';
import {
  DISABLED_TRIGGERS_KEY,
  commandHeadTrigger,
  formatDisabledTriggers,
  moduleAllowedTriggers,
  moduleCommandTriggerMap,
  moduleDef,
  normalizeTrigger,
  parseDisabledTriggers,
  toggleTriggersInList
} from './index';
import type { ModuleDef } from './module-def';

describe('normalizeTrigger and commandHeadTrigger', () => {
  test.each([
    { raw: '!bagels', want: 'bagels' },
    { raw: '  !BagelCount  ', want: 'bagelcount' },
    { raw: 'fed', want: 'fed' },
    { raw: '!mraffle [amount] [seconds] [winners]', want: 'mraffle' },
    { raw: '!cmd add <name> <response>', want: 'cmd' },
    { raw: '!', want: '' },
    { raw: '   ', want: '' }
  ])('normalizes $raw -> $want', ({ raw, want }) => {
    expect(normalizeTrigger(raw)).toBe(want);
    expect(commandHeadTrigger({ trigger: raw })).toBe(want);
  });
});

describe('parseDisabledTriggers and formatDisabledTriggers', () => {
  test.each([
    { raw: undefined, want: [] },
    { raw: '', want: [] },
    { raw: ' !Fed , bagelcount, , !fed ', want: ['bagelcount', 'fed'] }
  ])('parses $raw', ({ raw, want }) => {
    expect([...parseDisabledTriggers(raw)].sort()).toEqual(want);
  });

  test.each([
    { input: ['!Fed', 'bagelcount', 'fed', '', ' ! '], allowed: undefined, want: 'bagelcount,fed' },
    { input: ['fed', 'unknown', 'bagels'], allowed: new Set(['bagels', 'fed']), want: 'bagels,fed' },
    { input: [], allowed: new Set(['bagels']), want: '' }
  ])('formats $input -> $want', ({ input, allowed, want }) => {
    expect(formatDisabledTriggers(input, allowed)).toBe(want);
  });
});

describe('toggleTriggersInList', () => {
  const allowed = new Set(['bagels', 'fed', 'bagelcount', 'bagelboard']);

  test.each([
    { raw: undefined, triggers: ['!fed'], enabled: false, want: 'fed' },
    { raw: 'fed', triggers: ['!bagelcount'], enabled: false, want: 'bagelcount,fed' },
    { raw: 'bagelcount,fed', triggers: ['fed'], enabled: true, want: 'bagelcount' },
    { raw: 'fed', triggers: ['!fed'], enabled: true, want: '' },
    { raw: 'stale,fed', triggers: ['bagels'], enabled: false, want: 'bagels,fed' },
    { raw: undefined, triggers: ['!bagels', '!fed', '!bagelcount'], enabled: false, want: 'bagelcount,bagels,fed' },
    { raw: 'bagelboard,bagelcount,bagels,fed', triggers: ['bagels', 'fed'], enabled: true, want: 'bagelboard,bagelcount' },
    { raw: 'bagels', triggers: ['!bagels', ''], enabled: true, want: '' }
  ])('toggles $triggers (enabled=$enabled)', ({ raw, triggers, enabled, want }) => {
    expect(toggleTriggersInList(raw, triggers, enabled, allowed)).toBe(want);
  });

  test('exports the stored config key name', () => {
    expect(DISABLED_TRIGGERS_KEY).toBe('disabledTriggers');
  });
});

describe('moduleCommandTriggerMap and moduleAllowedTriggers', () => {
  test('maps head commands with aliases and leaves subcommand rows empty', () => {
    const cmdMap = moduleCommandTriggerMap(moduleDef('cmd')!);
    expect(Object.fromEntries(cmdMap)).toEqual({
      '!cmd': ['cmd', 'cmds', 'command', 'commands'],
      '!cmd add <name> <response>': [],
      '!cmd edit <name> <response>': [],
      '!cmd remove <name>': [],
      '!cmd disable <name>': [],
      '!cmd enable <name>': [],
      '!cmd perm <name> <level>': []
    });
  });

  test('deduplicates repeated aliases across rows and skips non-toggleable modules', () => {
    const custom: ModuleDef = {
      id: 'sample',
      label: 'Sample',
      tagline: 'Sample',
      description: 'Sample',
      category: 'Chat',
      defaultEnabled: false,
      replies: [],
      commands: [
        { trigger: '!alpha', aliases: ['!beta', '!alpha', ''], summary: 'First' },
        { trigger: '!beta <arg>', aliases: ['!gamma'], summary: 'Claimed already' }
      ]
    };
    expect(Object.fromEntries(moduleCommandTriggerMap(custom))).toEqual({
      '!alpha': ['alpha', 'beta'],
      '!beta <arg>': []
    });
    expect([...moduleAllowedTriggers(custom)].sort()).toEqual(['alpha', 'beta']);
    expect(moduleAllowedTriggers(moduleDef('stream')!).size).toBe(0);
  });
});
