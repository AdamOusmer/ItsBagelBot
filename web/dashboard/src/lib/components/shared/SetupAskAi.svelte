<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import { getI18n } from '@bagel/kit/i18n/context';
  import { webHref } from '@bagel/kit/site-links';
  import { copyFlash } from '@bagel/ui/lib/clipboard';
  import Button from '@bagel/ui/svelte/Button.svelte';
  import TextLink from '@bagel/ui/svelte/TextLink.svelte';

  let { feature, step, error = '', redirectUri = '' }: {
    feature: 'spotify' | 'govee'; step: string; error?: string; redirectUri?: string;
  } = $props();
  const uid = $props.id();
  const { t, locale } = getI18n();
  const GUIDES = { spotify: '/guides/spotify-song-requests/', govee: '/guides/govee-lights/' };
  const FEATURES = { spotify: 'setupAi.featureSpotify', govee: 'setupAi.featureGovee' };
  const AIS = [
    { label: 'ChatGPT', href: 'https://chatgpt.com/' },
    { label: 'Claude', href: 'https://claude.ai/new' },
    { label: 'Gemini', href: 'https://gemini.google.com/app' }
  ];
  const url = $derived(webHref(locale === 'fr' ? 'fr' : 'en', GUIDES[feature]));
  let copied = $state(false);
  let failed = $state(false);
  const LABELS = ['setupAi.copy', 'setupAi.copied', 'setupAi.copyFailed'] as const;
  const status = $derived(copied ? 'setupAi.copied' : failed ? 'setupAi.copyFailed' : 'setupAi.copy');
  function prompt() {
    const lines = [
      t('setupAi.promptIntro', { feature: t(FEATURES[feature]) }),
      t('setupAi.promptStep', { step })
    ];
    if (error) lines.push(t('setupAi.promptError', { error }));
    if (redirectUri) lines.push(t('setupAi.promptRedirect', { uri: redirectUri }));
    lines.push(t('setupAi.promptGuide', { url }), t('setupAi.promptRules'), t('setupAi.promptPrivate'));
    return lines.join('\n');
  }
  async function copyPrompt() {
    failed = !(await copyFlash(prompt(), (on) => (copied = on), 2200));
  }
</script>

<div class="ask-ai">
  <span class="bb-sr-only" id={`${uid}-description`}>{t('setupAi.description')}</span>
  <div>
    <Button variant="secondary" size="sm" title={t('setupAi.description')} aria-describedby={`${uid}-description`} onclick={copyPrompt}>
      <span class="ask-ai-label">
        {#each LABELS as key (key)}<span aria-hidden={status !== key} class:is-hidden={status !== key}>{t(key)}</span>{/each}
      </span>
    </Button>
  </div>
  <div class="ask-ai-links">
    {#each AIS as ai (ai.href)}<TextLink variant="quiet" external href={ai.href} label={`${ai.label} ↗`} />{/each}
  </div>
</div>

<style>
  .ask-ai { display: grid; justify-items: center; align-content: start; gap: 6px; height: 64px; text-align: center; }
  .ask-ai-links { display: flex; align-items: center; justify-content: center; gap: 16px; }
  .ask-ai-label { display: grid; justify-items: center; }
  .ask-ai-label > span { grid-area: 1 / 1; }
  .is-hidden { visibility: hidden; }
</style>
