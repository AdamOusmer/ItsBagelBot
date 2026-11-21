// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { expect, mock, test } from 'bun:test';
import { createRequire } from 'node:module';
import { fileURLToPath } from 'node:url';
import * as svelte from 'svelte';
import { render } from 'svelte/server';
import { ALERT_VOICE_CONFIG_DEFAULTS } from '@bagel/kit/voice-plan';
import { stubSvelteKit } from '../../../../test/sveltekit';

stubSvelteKit();
const { translate } = await import('@bagel/kit/i18n');
const english = {
  t: (key: string, params?: Record<string, string | number>) => translate('en', key, params),
  tl: () => [],
  locale: 'en'
};
mock.module('@bagel/kit/i18n/context', () => ({
  getI18n: () => english,
  setI18n: () => english
}));
mock.module('@bagel/ui/svelte/discard-guard', () => ({
  createDiscardGuard: () => ({ open: false, guard: () => {}, confirm: () => {}, cancel: () => {} })
}));

const uiSvelte = createRequire(import.meta.resolve('@bagel/ui/svelte/Input.svelte')).resolve('svelte');
if (uiSvelte !== fileURLToPath(import.meta.resolve('svelte'))) mock.module(uiSvelte, () => svelte);

const { default: VoicePage } = await import('./+page.svelte');

const allowance = {
  plan: 'premium', reason: 'ok', limit_tokens: 90_000, used_tokens: 57_300, remaining_tokens: 32_700,
  resets_at: '2026-12-01T00:00:00Z', enabled: true, voice: 'Kore', max_seconds: 120
};

const data = {
  readOnly: false, locked: false, allowance, alerts: { config: ALERT_VOICE_CONFIG_DEFAULTS, revision: 9 }
};

function renderVoice(overrides: Record<string, unknown> = {}, form: Record<string, unknown> | null = null) {
  return render(VoicePage, { props: { data: { ...data, ...overrides }, form } as never }).body;
}

test.each([
  { stored: undefined, displayed: '120' },
  { stored: 5, displayed: '5' },
  { stored: 45, displayed: '45' },
  { stored: 120, displayed: '120' }
])('renders a required whole-second input from 5 to 120 with $displayed selected', async ({ stored, displayed }) => {
  const html = renderVoice({ allowance: { ...allowance, max_seconds: stored } });
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

test('shows the shared Premium gate for an owner without a plan', () => {
  const html = renderVoice({ allowance: { ...allowance, plan: 'free', reason: 'no_plan' }, locked: true, alerts: null });
  expect(html).toContain('Give your alerts a voice with Premium');
  expect(html).not.toContain('action="?/save"');
});

test('renders usage, the alert editors and their initial states for a Premium owner', () => {
  const html = renderVoice();
  expect([
    '38 min 12 s of 60 min', '21 min 48 s left', 'bb-progress--inset',
    'name="followTts" value="false"', 'name="subTts" value="true"', 'Plays at 100 bits or more'
  ].filter((text) => !html.includes(text))).toEqual([]);
});

const obsURL = 'https://overlay.itsbagelbot.com/tts#AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA';

test.each([
  { name: 'owner before creation', readOnly: false, configured: false, limit: 90_000, url: undefined, visible: true },
  { name: 'owner revisiting', readOnly: false, configured: true, limit: 90_000, url: undefined, visible: true },
  { name: 'owner after rotation', readOnly: false, configured: true, limit: 90_000, url: obsURL, visible: true },
  { name: 'zero allowance', readOnly: false, configured: false, limit: 0, url: undefined, visible: false },
  { name: 'read-only session', readOnly: true, configured: true, limit: 90_000, url: undefined, visible: false }
])('renders the appropriate OBS controls for $name', async ({ readOnly, configured, limit, url, visible }) => {
  const form = url ? { overlayUrl: url } : null;
  const html = renderVoice({ readOnly, allowance: { ...allowance, limit_tokens: limit, overlay_configured: configured } }, form);
  let submitLabel = '';
  const rewriter = new HTMLRewriter().on('form[action="?/rotateOverlay"] button[type="submit"]', {
    text(chunk) {
      submitLabel += chunk.text;
    }
  });
  await rewriter.transform(new Response(html)).text();

  const buttonLabel = configured ? 'Make a new link' : 'Create link';
  expect(submitLabel.trim()).toBe(visible ? buttonLabel : '');
  expect(html.includes('itsbagelbot.com/guides/premium-voice')).toBe(visible);
  expect(html.includes(obsURL)).toBe(Boolean(url));
});
