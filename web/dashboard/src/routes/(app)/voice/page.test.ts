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
mock.module('#lib/autosave.svelte', () => ({
  Autosave: class {
    status = {};
    save() {}
    schedule() {}
    flush() {}
    reset() {}
  }
}));

const uiSvelte = createRequire(import.meta.resolve('@bagel/ui/svelte/Input.svelte')).resolve('svelte');
if (uiSvelte !== fileURLToPath(import.meta.resolve('svelte'))) mock.module(uiSvelte, () => svelte);

const { default: VoicePage } = await import('./+page.svelte');

const allowance = {
  plan: 'premium', reason: 'ok', limit_tokens: 90_000, used_tokens: 57_300, remaining_tokens: 32_700,
  resets_at: '2026-12-01T00:00:00Z'
};

const overlay = { settings: { enabled: true, voice: 'Kore', max_seconds: 120 }, configured: true, revision: 1 };

const data = {
  readOnly: false, locked: false, allowance, overlay, alerts: { config: ALERT_VOICE_CONFIG_DEFAULTS, revision: 9 }
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
  const html = renderVoice({ overlay: { ...overlay, settings: { ...overlay.settings, max_seconds: stored } } });
  const inputs: Record<string, string>[] = [];
  const rewriter = new HTMLRewriter().on('input[name="max_seconds"]', {
    element(input) {
      inputs.push(Object.fromEntries(input.attributes));
    }
  });
  await rewriter.transform(new Response(html)).text();
  expect(inputs).toEqual([expect.objectContaining({
    type: 'number', min: '5', max: '120', step: '1', required: '', value: displayed
  })]);
});

test('shows the voice stage with direct checkout for an owner without a plan', () => {
  const html = renderVoice({ allowance: { ...allowance, plan: 'free', reason: 'no_plan' }, locked: true, alerts: null });
  expect({
    title: html.includes(translate('en', 'voice.lockedTitle')) && html.includes(translate('en', 'nav.voice')),
    orb: html.includes('bb-audio-orb'),
    wheel: html.includes('role="listbox"'),
    checkout: html.includes('action="/billing?/subscribe"'),
    settings: html.includes('name="max_seconds"')
  }).toEqual({ title: true, orb: true, wheel: true, checkout: true, settings: false });
});

test('the voice wheel starts on the saved default voice and marks it', async () => {
  const html = renderVoice({ overlay: { ...overlay, settings: { ...overlay.settings, voice: 'Puck' } } });
  let marked = '';
  const rewriter = new HTMLRewriter().on('[role="option"]:not([aria-hidden="true"])', {
    element(option) {
      if (option.getAttribute('aria-selected') === 'true') marked = option.getAttribute('id') ?? '';
    }
  });
  await rewriter.transform(new Response(html)).text();
  expect({ active: html.includes('aria-activedescendant="voice-wheel-1-Puck"'), marked }).toEqual({
    active: true, marked: 'voice-wheel-1-Puck'
  });
});

test('renders usage, autosaved settings rows and one alert row per message-carrying kind', async () => {
  const config = { ...ALERT_VOICE_CONFIG_DEFAULTS, cheerTts: 'false' };
  const html = renderVoice({ alerts: { config, revision: 9 } });
  const switches: Record<string, string> = {};
  const rows: string[] = [];
  const rewriter = new HTMLRewriter()
    .on('[role="switch"][aria-label]', {
      element(control) { switches[control.getAttribute('aria-label') ?? ''] = control.getAttribute('aria-checked') ?? ''; }
    })
    .on('ul[aria-labelledby="voice-alerts"] [id^="voice-kind-"]', {
      element(heading) { rows.push(heading.getAttribute('id') ?? ''); }
    });
  await rewriter.transform(new Response(html)).text();

  expect([
    '38 min 12 s of 60 min', '21 min 48 s left', 'bb-progress--inset', 'Changes save automatically',
    'Plays at 100 bits or more', 'Plays on resubs with a message'
  ].filter((text) => !html.includes(text))).toEqual([]);
  expect(['Save changes', 'voice-kind-follow', 'voice-kind-gift', 'voice-kind-raid', 'Read the viewer']
    .filter((text) => html.includes(text))).toEqual([]);
  expect(rows).toEqual(['sub', 'cheer'].map((kind) => `voice-kind-${kind}`));
  expect(['Cheer', 'Subscription'].map((label) => switches[translate('en', 'modules.toggleAria', { label })]))
    .toEqual(['false', 'true']);
});

test('renders each alert style picker without a rehearsal or spoken message textarea', async () => {
  const styleValues: string[] = [];
  let textareas = 0;
  const rewriter = new HTMLRewriter()
    .on('.bb-select__fallback select[name="subTtsStyle"] option', {
      element(option) { styleValues.push(option.getAttribute('value') ?? ''); }
    })
    .on('textarea', {
      element() { textareas += 1; }
    });
  const html = renderVoice();
  await rewriter.transform(new Response(html)).text();
  expect({ styleValues, textareas, rehearsal: html.includes('Read aloud') }).toEqual({
    styleValues: ['warm', 'hype', 'calm', 'playful', 'cozy'],
    textareas: 0,
    rehearsal: false
  });
});

const obsURL = 'https://overlay.itsbagelbot.com/tts#AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA';

async function collectObsControls(html: string) {
  let submitLabel = '';
  let testLabel = '';
  let testDisabled: boolean | undefined;
  let firstStep = '';
  const rewriter = new HTMLRewriter()
    .on('form[action="?/rotateOverlay"] button[type="submit"]', {
      text(chunk) { submitLabel += chunk.text; }
    })
    .on('form[action="?/testAlert"] button[type="submit"]', {
      element(button) { testDisabled = button.hasAttribute('disabled'); },
      text(chunk) { testLabel += chunk.text; }
    })
    .on('.bb-steps--checklist > li:first-child .bb-step__label', {
      text(chunk) { firstStep += chunk.text; }
    });
  await rewriter.transform(new Response(html)).text();
  return { submitLabel, testLabel, testDisabled, firstStep };
}

test.each([
  { name: 'owner before creation', readOnly: false, configured: false, limit: 90_000, url: undefined, visible: true },
  { name: 'owner revisiting', readOnly: false, configured: true, limit: 90_000, url: undefined, visible: true },
  { name: 'owner after rotation', readOnly: false, configured: false, limit: 90_000, url: obsURL, visible: true },
  { name: 'zero allowance', readOnly: false, configured: false, limit: 0, url: undefined, visible: false },
  { name: 'read-only session', readOnly: true, configured: true, limit: 90_000, url: undefined, visible: false }
])('renders the appropriate OBS controls for $name', async ({ readOnly, configured, limit, url, visible }) => {
  const form = url ? { overlayUrl: url } : null;
  const html = renderVoice({ readOnly, allowance: { ...allowance, limit_tokens: limit }, overlay: { ...overlay, configured } }, form);
  const { submitLabel, testLabel, testDisabled, firstStep } = await collectObsControls(html);

  expect({
    button: submitLabel.trim(),
    testLabel: testLabel.trim(),
    testDisabled,
    firstStep: firstStep.trim(),
    guide: html.includes('itsbagelbot.com/guides/premium-voice'),
    link: html.includes(obsURL),
    done: html.includes('bb-step__check'),
    stateLabel: html.includes('bb-step__state')
  }).toEqual(expectedObsControls(visible, Boolean(url) || configured, Boolean(url)));
});

function expectedObsControls(visible: boolean, hasLink: boolean, link: boolean) {
  const shown = { button: hasLink ? 'Make a new link' : 'Create link', testLabel: 'Send test alert', testDisabled: !hasLink,
    firstStep: 'Generate a private link and copy it when it appears.' };
  const hidden = { button: '', testLabel: '', testDisabled: undefined, firstStep: '' };
  return { ...(visible ? shown : hidden), guide: visible, link, done: visible && hasLink, stateLabel: false };
}

const safety = {
  manual: [{ viewer_id: '5', viewer_login: 'spammer', created_at: 1_764_000_000 }],
  pausedUntil: 4_102_444_800_000
};

test.each([
  { name: 'with blocks and a paused channel', data: { safety }, present: ['Blocked viewers', 'spammer', 'Voice is paused for this channel until Jan 1, 2100'], hidden: [] },
  { name: 'without safety data', data: { safety: null }, present: [], hidden: ['Blocked viewers', 'Voice is paused for this channel'] }
])('renders the viewer block list $name', ({ data: extra, present, hidden }) => {
  const html = renderVoice(extra);
  expect([
    ...present.filter((text) => !html.includes(text)),
    ...hidden.filter((text) => html.includes(text))
  ]).toEqual([]);
});

test('keeps the unblock control for each manual block', async () => {
  const actions: string[] = [];
  const rewriter = new HTMLRewriter().on('form[action^="?/unblock"]', {
    element(form) { actions.push(form.getAttribute('action') ?? ''); }
  });
  await rewriter.transform(new Response(renderVoice({ safety }))).text();
  expect(actions).toEqual(['?/unblockViewer']);
});
