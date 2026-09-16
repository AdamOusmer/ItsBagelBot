// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { describe, expect, test } from 'bun:test';
import en from './i18n/locales/en.json';
import fr from './i18n/locales/fr.json';
import { MODULE_CATALOG, moduleDef, PERM_LABELS, PERMS } from './types';
import {
  catalogKey,
  commandSummarySlug,
  tCatalog,
  tModuleCommandSummary,
  tModuleDescription,
  tModuleFieldOption,
  tModuleFieldPart,
  tModuleLabel,
  tModuleReplyPart,
  tModuleTagline,
  tPerm,
  tPermBadge
} from './module-copy';

type Tree = Record<string, unknown>;

function lookup(tree: unknown, key: string): string | undefined {
  let node: unknown = tree;
  for (const part of key.split('.')) {
    if (node === null || typeof node !== 'object') return undefined;
    node = (node as Tree)[part];
  }
  return typeof node === 'string' ? node : undefined;
}

function translator(tree: unknown) {
  return (key: string) => lookup(tree, key) ?? key;
}

const tEn = translator(en);
const tFr = translator(fr);

describe('catalog i18n overlay', () => {
  test('a missing key falls back to the catalog English, never the dotted path', () => {
    expect(tCatalog(tEn, 'modules.catalog.nope.label', 'Fallback')).toBe('Fallback');
  });

  test('every catalog module has label, tagline and description matching English', () => {
    for (const def of MODULE_CATALOG) {
      expect(tModuleLabel(tEn, def)).toBe(def.label);
      expect(tModuleTagline(tEn, def)).toBe(def.tagline);
      expect(tModuleDescription(tEn, def)).toBe(def.description);
    }
  });

  test('overlay leaves that exist match the English catalog copy they overlay', () => {
    for (const def of MODULE_CATALOG) {
      for (const field of def.settings ?? []) {
        for (const part of ['label', 'help', 'placeholder'] as const) {
          const fallback = field[part] ?? '';
          if (!fallback) continue;
          const over = lookup(en, catalogKey(def.id, 'settings', field.key, part));
          if (over !== undefined) expect(over).toBe(fallback);
          expect(tModuleFieldPart(tEn, def.id, field, part)).toBe(fallback);
        }
        for (const opt of field.options ?? []) {
          const over = lookup(en, catalogKey(def.id, 'settings', field.key, 'options', opt.value));
          if (over !== undefined) expect(over).toBe(opt.label);
          expect(tModuleFieldOption(tEn, def.id, field.key, opt)).toBe(opt.label);
        }
      }
      for (const reply of def.replies) {
        for (const part of ['label', 'tagline', 'event'] as const) {
          const over = lookup(en, catalogKey(def.id, 'replies', reply.key, part));
          if (over !== undefined) expect(over).toBe(reply[part]);
          expect(tModuleReplyPart(tEn, def.id, reply, part)).toBe(reply[part]);
        }
      }
      for (const command of def.commands ?? []) {
        const over = lookup(en, catalogKey(def.id, 'commands', commandSummarySlug(command.trigger)));
        if (over !== undefined) expect(over).toBe(command.summary);
        expect(tModuleCommandSummary(tEn, def.id, command)).toBe(command.summary);
      }
    }
  });

  test('French overlay changes CODM copy without losing the command names', () => {
    const def = moduleDef('codm');
    expect(def).toBeDefined();
    if (!def) return;
    expect(tModuleLabel(tFr, def)).toBe('Profil CODM');
    expect(tModuleTagline(tFr, def)).toContain('Call of Duty: Mobile');
    expect(tModuleFieldPart(tFr, def.id, def.settings![0], 'label')).toBe('Compte CODM lié');
    expect(tModuleReplyPart(tFr, def.id, def.replies[0], 'event')).toBe('!codm [UID/pseudo exact]');
  });

  test('linkedOnly falls back to the shared overlay when a module does not override it', () => {
    const def = moduleDef('fortnite');
    expect(def).toBeDefined();
    if (!def) return;
    const linked = def.settings!.find((field) => field.key === 'linkedOnly');
    expect(linked).toBeDefined();
    if (!linked) return;
    expect(tModuleFieldPart(tEn, def.id, linked, 'label')).toBe(linked.label);
    expect(tModuleFieldPart(tFr, def.id, linked, 'label')).toBe('Ne chercher que mon compte lié');
  });

  test('command summary slugs stay stable for the queue inspector', () => {
    expect(commandSummarySlug('!queue next')).toBe('queue_next');
    expect(commandSummarySlug('!gamble <amount>')).toBe('gamble_amount');
  });

  test('catalogKey builds the overlay path the locales actually ship', () => {
    expect(lookup(en, catalogKey('codm', 'settings', 'account', 'placeholder'))).toBe(
      'CODM UID or exact nickname'
    );
  });

  test('permission labels overlay in both locales', () => {
    for (const perm of PERMS) {
      expect(tPerm(tEn, perm)).toBe(PERM_LABELS[perm]);
    }
    expect(tPerm(tFr, 'everyone')).toBe('Tout le monde');
    expect(tPerm(tFr, 'lead_mod')).toBe('Modérateurs principaux');
    expect(tPermBadge(tEn, 'sub')).toBe('Subs');
    expect(tPermBadge(tFr, 'sub')).toBe('Abos');
  });
});
