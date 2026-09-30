// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { describe, expect, test } from 'bun:test';
import { assembleCatalogs, catalogLocale } from './tree';

const file = (path: string, content: unknown) => ({ path, content });

describe('assembleCatalogs', () => {
  test('derives the key prefix from the path below <code>/console/', () => {
    const trees = assembleCatalogs([
      file('../../../../locales/en/console/commands.json', { title: 'Commands', row: { edit: 'Edit' } }),
      file('en/console/admin/index.json', { title: 'Admin' }),
      file('en/console/admin/users.json', { pagerNext: 'Next' }),
      file('en/console/index.json', { root: 'Root' }),
      file('en/console/lists.json', { 'a.b': ['x', 'y'] })
    ]);
    expect(trees.en).toEqual({
      commands: { title: 'Commands', row: { edit: 'Edit' } },
      admin: { title: 'Admin', users: { pagerNext: 'Next' } },
      root: 'Root',
      lists: { a: { b: ['x', 'y'] } }
    });
  });

  test('keeps locales apart', () => {
    const trees = assembleCatalogs([file('en/console/lang.json', { name: 'English' }), file('fr/console/lang.json', { name: 'Français' })]);
    expect(trees).toEqual({ en: { lang: { name: 'English' } }, fr: { lang: { name: 'Français' } } });
  });

  test('throws when two files define the same key', () => {
    expect(() =>
      assembleCatalogs([file('en/console/admin/index.json', { title: 'A' }), file('en/console/admin.json', { title: 'B' })])
    ).toThrow('redefines admin.title');
  });

  test('throws when a leaf and a branch claim the same key, in either order', () => {
    const branch = file('en/console/admin/users.json', { next: 'Next' });
    expect(() => assembleCatalogs([branch, file('en/console/admin/index.json', { users: 'Users' })])).toThrow(
      'overlaps the key admin.users'
    );
    expect(() => assembleCatalogs([branch, file('en/console/index.json', { admin: { users: 'Users' } })])).toThrow(
      'redefines admin.users'
    );
  });

  test('rejects files outside a console tree and non-object content', () => {
    expect(() => assembleCatalogs([file('en/chat/core.json', {})])).toThrow('not a locales/<code>/console/ catalog file');
    expect(() => assembleCatalogs([file('en/console/x.json', ['a'])])).toThrow('must hold a JSON object');
  });
});

describe('catalogLocale', () => {
  test('reads the locale code from a glob path', () => {
    expect(catalogLocale('../../../../locales/fr/console/admin/deploys/index.json')).toBe('fr');
    expect(catalogLocale('../../../../locales/fr/chat/core.json')).toBeUndefined();
  });
});
