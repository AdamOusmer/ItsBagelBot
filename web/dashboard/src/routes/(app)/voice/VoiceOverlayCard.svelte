<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  import { enhance, type SubmitFunction } from '$app/forms';
  import { getI18n } from '@bagel/kit/i18n/context';
  import { SITE } from '@bagel/kit/site-links';
  import Button from '@bagel/ui/svelte/Button.svelte';
  import Card from '@bagel/ui/svelte/Card.svelte';
  import CopySurface from '@bagel/ui/svelte/CopySurface.svelte';
  import Heading from '@bagel/ui/svelte/Heading.svelte';
  import StepList, { type StepItem } from '@bagel/ui/svelte/StepList.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import TextLink from '@bagel/ui/svelte/TextLink.svelte';

  let { configured, url, error, busy, disabled, submit }: {
    configured: boolean;
    url?: string;
    error?: string;
    busy: boolean;
    disabled: boolean;
    submit: SubmitFunction;
  } = $props();

  const { t } = getI18n();
  const steps: StepItem[] = $derived([
    { id: 'create', label: t('voice.overlayStepCreate'), state: configured ? 'succeeded' : 'pending' },
    { id: 'add', label: t('voice.overlayStepAdd'), state: 'pending' },
    { id: 'keep', label: t('voice.overlayStepKeep'), state: 'pending' }
  ]);
  let failedUrl = $state<string>();
  const status = $derived(error ?? (failedUrl && failedUrl === url ? t('voice.overlayCopyFailed') : ''));
</script>

<Card as="section" aria-labelledby="voice-overlay-title">
  <div class="overlay">
    <Heading level={6} as="h2" id="voice-overlay-title">
      <bb-fit><span data-fit="">{t('voice.overlayTitle')}</span></bb-fit>
    </Heading>
    <StepList {steps} fitCopy checklist />
    <div class="guide">
      <TextLink href={`${SITE.web}/guides/premium-voice`} variant="quiet" label={t('voice.overlayGuide')} fit />
    </div>
    <form method="POST" action="?/rotateOverlay" use:enhance={submit}>
      <div class="overlay-actions">
        <Button variant="secondary" size="sm" type="submit" {busy} {disabled}>
          <bb-fit><span data-fit="">{configured ? t('voice.overlayReplace') : t('voice.overlayCreate')}</span></bb-fit>
        </Button>
        <div class="overlay-link" class:available={!!url} inert={!url}>
          <CopySurface variant="well" text={url ?? ''} aria-label={t('voice.overlayLink')}
            disabled={busy} legacyFallback onCopy={(copied) => (failedUrl = copied ? undefined : url)} />
        </div>
      </div>
    </form>
    <div class="overlay-feedback">
      <div class="feedback-text bb-fit-box" class:visible={busy || !status} aria-hidden={!busy && !!status}>
        <Text as="div" size="sm" tone="muted"><bb-fit mode="block">{t('voice.overlayHint')}</bb-fit></Text>
      </div>
      <div class="feedback-text bb-fit-box" class:visible={!busy && !!status} role="status">
        <Text as="div" size="sm" tone="danger"><bb-fit mode="block">{busy ? '' : status}</bb-fit></Text>
      </div>
    </div>
  </div>
</Card>

<style>
  .overlay {
    display: grid;
    gap: var(--bb-space-2);
    grid-template-columns: minmax(0, 1fr);
  }
  .overlay-feedback {
    display: grid;
    grid-template-columns: minmax(0, 1fr);
  }
  .feedback-text {
    grid-area: 1 / 1;
    block-size: 2rem;
    min-inline-size: 0;
    align-content: center;
    opacity: 0;
  }
  .feedback-text.visible { opacity: 1; }
  .overlay-actions {
    display: grid;
    grid-template-columns: minmax(0, min(50%, 11rem)) minmax(0, 1fr);
    align-items: center;
    gap: var(--bb-space-2);
    --btn-w: 100%;
    --btn-justify: center;
  }
  .overlay-link {
    min-inline-size: 0;
    opacity: 0;
  }
  .overlay-link.available { opacity: 1; }
  .guide { block-size: 1.5rem; }
  @media (prefers-reduced-motion: no-preference) {
    .overlay-link, .feedback-text { transition: opacity var(--bb-dur-fast) var(--bb-ease-out-expo); }
  }
</style>
