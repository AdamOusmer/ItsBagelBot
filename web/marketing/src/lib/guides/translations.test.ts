import { describe, expect, test } from 'bun:test';
import { readdirSync } from 'node:fs';
import { guideIds, translate } from './translate';

const folder = new URL('../../content/guides/', import.meta.url);
const names = readdirSync(folder);

describe('data-only guide translations', () => {
  for (const name of names.filter((name) => name.endsWith('.json') && !name.startsWith('hub.'))) {
    test(name, async () => {
      const slug = name.split('.')[0];
      const english = (await import(new URL(`${slug}.en.ts`, folder).href)).default;
      const copy = await Bun.file(new URL(name, folder)).json();
      expect(Array.isArray(copy)).toBe(false);
      expect(Object.values(copy).every((value) => typeof value === 'string')).toBe(true);
      const { ids, blocks } = guideIds(english);
      for (const id of ids) expect(copy[id]).toBeString();
      for (const id of Object.keys(copy)) {
        const marker = id.indexOf('.labels.');
        expect(ids.includes(id) || (marker > 0 && blocks.includes(id.slice(0, marker)))).toBe(true);
      }
      const localized = translate(english, copy);
      expect(localized.sections.map((section) => section.id)).toEqual(english.sections.map((section) => section.id));
      expect(localized.meta.title).toBe(copy['meta.title']);
    });
  }
  test('French hub preserves navigation and content structure', async () => {
    const en = await Bun.file(new URL('hub.en.json', folder)).json();
    const fr = await Bun.file(new URL('hub.fr.json', folder)).json();
    const shape = (value: unknown): unknown => Array.isArray(value)
      ? value.map(shape)
      : value && typeof value === 'object'
        ? Object.fromEntries(Object.entries(value).map(([key, child]) => [key, shape(child)]))
        : typeof value;
    expect(shape(fr)).toEqual(shape(en));
  });
});
