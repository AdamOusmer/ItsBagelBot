// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
import { expect, test } from 'bun:test';
import { astroHtml, svelteHtml } from './contract';
import { normalise } from './normalise';
import SvelteSky from '../svelte/AmbientSky.svelte';
import StageSky from '../svelte/Sky.svelte';
import AstroSky from '../astro/AmbientSky.astro';

for (const props of [
  {},
  { position: 'contained', shift: -0.4, px: 0.2, py: -0.3, progress: 1.5, leaving: true, warmth: 0.4, class: 'preview', 'data-preview': 'sky' },
  { progress: -1 },
]) {
  test(`AmbientSky adapters agree: ${JSON.stringify(props)}`, async () => {
    const svelte = svelteHtml(SvelteSky, props);
    expect(svelte).toBe(await astroHtml(AstroSky, props));
    expect(svelte).toContain('aria-hidden="true"');
    expect(svelte).toContain('class="bb-light-field" data-field');
    expect(svelte).not.toContain('bb-ambient-sky__ring');
    expect(svelte).toContain(`--ambient-progress: ${Math.min(Math.max(props.progress ?? 0, 0), 1)};`);
  });
}

for (const props of [{}, { shift: -1, px: 0.25, py: -0.2, progress: 0.8, leaving: true }]) {
  test(`Sky stage wrapper preserves the shared atmosphere: ${JSON.stringify(props)}`, () => {
    const wrapper = normalise(svelteHtml(StageSky, props));
    const direct = normalise(svelteHtml(SvelteSky, props));
    expect(wrapper).toBe(direct);
    expect(wrapper).toContain('bb-ambient-sky--fixed');
    expect(wrapper).toContain('data-warmth="0.7"');
  });
}
