import { describe, expect, test } from 'bun:test';
import { readdirSync } from 'node:fs';
import { guideIds, translate } from './translate';
import type { GuideContent } from './types';

const folder = new URL('../../content/guides/', import.meta.url);
const names = readdirSync(folder);

function expectBedwarsTagCommands(guide: GuideContent) {
  const widgets = guide.sections.flatMap((section) => section.blocks)
    .filter((block) => block.kind === 'widget');
  const widget = widgets.find((block) => block.name === 'ModuleCatalog');
  if (!widget) throw new Error('ModuleCatalog widget is missing');
  const modules = widget.props?.modules as { commands?: string }[];
  const commands = modules.find((module) => module.commands?.includes('!bwstats'))?.commands?.split(', ');
  expect(commands).toContain('!bwtag');
  expect(commands).toContain('!bwtags');
  expect(commands).toContain('!bw tag');
  expect(commands).toContain('!bedwars tag');
  expect(commands).not.toContain('!tag');
  expect(commands).not.toContain('!tags');
}

describe('data-only guide translations', () => {
  for (const name of names.filter((name) => name.endsWith('.json') && !name.startsWith('hub.'))) {
    test(name, async () => {
      const slug = name.split('.')[0];
      const english = (await import(new URL(`${slug}.en.ts`, folder).href)).default;
      const copy = await Bun.file(new URL(name, folder)).json();
      expect(Array.isArray(copy)).toBe(false);
      expect(Object.values(copy).every((value) => typeof value === 'string')).toBe(true);
      const { ids, blocks } = guideIds(english);
      expect(ids).not.toContain('meta.aiDoc');
      for (const id of ids) expect(copy[id]).toBeString();
      for (const id of Object.keys(copy)) {
        const marker = id.indexOf('.labels.');
        expect(ids.includes(id) || (marker > 0 && blocks.includes(id.slice(0, marker)))).toBe(true);
      }
      const localized = translate(english, copy);
      if (slug === 'modules') {
        expectBedwarsTagCommands(english);
        expectBedwarsTagCommands(localized);
      }
      expect(localized.sections.map((section) => section.id)).toEqual(english.sections.map((section) => section.id));
      expect(localized.meta.title).toBe(copy['meta.title']);
      expect(localized.meta.aiDoc).toBe(english.meta.aiDoc);
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
