// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
import { describe, expect, test } from 'bun:test';
import { featurePresetChanges } from './feature-presets';
import { BUILTIN_COMMANDS } from './catalog/builtin-commands';
import { MODULE_CATALOG } from './catalog';

describe('role preset changes', () => {
  test('new streamer restores shipped switches while import leaves saved states untouched', () => {
    const rows = [
      { name: 'clip', is_enabled: false },
      { name: 'personality', is_enabled: false },
      { name: 'songqueue', is_enabled: true },
      { name: 'custom-command', is_enabled: true }
    ];
    const changes = featurePresetChanges('start-new', rows);
    expect(changes).toContainEqual({ name: 'clip', enabled: true });
    expect(changes).toContainEqual({ name: 'personality', enabled: true });
    expect(changes).toContainEqual({ name: 'songqueue', enabled: false });
    expect(changes.some(({ name }) => name === 'custom-command')).toBe(false);
    expect(featurePresetChanges('import', rows)).toEqual([]);
  });

  test('integration disables built-in commands except clip and keeps module states', () => {
    const rows = [
      { name: 'clip', is_enabled: false },
      { name: 'songqueue', is_enabled: true },
      { name: 'personality', is_enabled: false },
      { name: 'custom-command', is_enabled: true }
    ];
    const changes = featurePresetChanges('integrate', rows);
    expect(changes.map(({ name }) => name).sort()).toEqual(
      BUILTIN_COMMANDS.filter(({ id, defaultActive }) => id !== 'clip' && defaultActive).map(({ id }) => id).sort()
    );
    expect(changes.every(({ enabled }) => enabled === false)).toBe(true);
    expect(featurePresetChanges('integrate', [
      ...rows,
      ...changes.map(({ name, enabled }) => ({ name, is_enabled: enabled }))
    ])).toEqual([]);
  });

  test('quiet disables every enabled toggleable module and built-in, including clip', () => {
    const rows = [
      { name: 'clip', is_enabled: true },
      { name: 'triggers', is_enabled: true },
      { name: 'songqueue', is_enabled: true },
      { name: 'custom-command', is_enabled: true }
    ];
    const changes = featurePresetChanges('quiet', rows);
    const enabledByDefault = MODULE_CATALOG.filter((def) => def.toggleable !== false && !def.hidden && def.defaultEnabled)
      .map(({ id }) => id);
    for (const name of [...enabledByDefault, 'clip', 'triggers', 'songqueue']) {
      expect(changes).toContainEqual({ name, enabled: false });
    }
    expect(changes.some(({ name }) => name === 'custom-command' || name === 'stream' || name === 'counters')).toBe(false);
  });

  test('song requests disable shipped responders and enable the queue', () => {
    const changes = featurePresetChanges('songs', []);
    expect(changes).toContainEqual({ name: 'songqueue', enabled: true });
    expect(changes).toContainEqual({ name: 'personality', enabled: false });
    expect(changes).toContainEqual({ name: 'alerts', enabled: false });
    expect(changes).toContainEqual({ name: 'followage', enabled: false });
    expect(changes.some((change) => change.name === 'stream' || change.name === 'counters')).toBe(false);
  });

  test('quiet disables an enabled optional module and song requests while leaving missing opt-ins alone', () => {
    const changes = featurePresetChanges('quiet', [
      { name: 'triggers', is_enabled: true },
      { name: 'songqueue', is_enabled: true },
      { name: 'alerts', is_enabled: false }
    ]);
    expect(changes).toContainEqual({ name: 'triggers', enabled: false });
    expect(changes).toContainEqual({ name: 'songqueue', enabled: false });
    expect(changes.some((change) => change.name === 'alerts')).toBe(false);
    expect(changes.some((change) => change.name === 'govee')).toBe(false);
  });
});
