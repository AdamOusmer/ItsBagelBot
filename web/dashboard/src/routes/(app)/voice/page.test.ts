// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { expect, mock, test } from 'bun:test';
import { createRequire } from 'node:module';
import { fileURLToPath } from 'node:url';
import * as svelte from 'svelte';
import { render } from 'svelte/server';
import { stubSvelteKit } from '../../../../test/sveltekit';

stubSvelteKit();
const uiSvelte = createRequire(import.meta.resolve('@bagel/ui/svelte/Input.svelte')).resolve('svelte');
if (uiSvelte !== fileURLToPath(import.meta.resolve('svelte'))) mock.module(uiSvelte, () => svelte);

const { default: VoicePage } = await import('./+page.svelte');

test.each([
  { stored: undefined, displayed: '120' },
  { stored: 5, displayed: '5' },
  { stored: 45, displayed: '45' },
  { stored: 120, displayed: '120' }
])('renders a required whole-second input from 5 to 120 with $displayed selected', async ({ stored, displayed }) => {
  const allowance = {
    plan: 'premium', reason: 'ok', limit_tokens: 90_000, used_tokens: 20_025,
    resets_at: '2026-12-01T00:00:00Z', enabled: true, voice: 'Kore', max_seconds: stored
  };
  const html = render(VoicePage, { props: { data: { readOnly: false, allowance }, form: null } as never }).body;
  const inputs: Record<string, string>[] = [];
  const rewriter = new HTMLRewriter().on('form[action="?/save"]:not([novalidate]) input[name="max_seconds"]', {
    element(input) {
      inputs.push(Object.fromEntries(input.attributes));
    }
  });
  await rewriter.transform(new Response(html)).text();
  expect(inputs).toEqual([expect.objectContaining({
    type: 'number', min: '5', max: '120', step: '1', required: '', value: displayed
  })]);
});
