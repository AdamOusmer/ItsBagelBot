<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  import { enhance, type SubmitFunction } from '$app/forms';
  import '@bagel/ui/styles/elements/collapse.css';
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
    <TextLink href={`${SITE.web}/guides/premium-voice`} variant="quiet" label={t('voice.overlayGuide')} fit />
    <form class="overlay-form" method="POST" action="?/rotateOverlay" use:enhance={submit}>
      <div class="overlay-actions">
        <Button variant="secondary" size="sm" type="submit" {busy} {disabled}>
          <bb-fit class="overlay-action-label"><span data-fit="">{configured ? t('voice.overlayReplace') : t('voice.overlayCreate')}</span></bb-fit>
        </Button>
      </div>
      <div class="bb-collapse" class:is-open={!!url} aria-hidden={!url} inert={!url}>
        <div>
          <CopySurface variant="well" text={url ?? ''} aria-label={t('voice.overlayLink')}
            disabled={busy} legacyFallback onCopy={(copied) => (failedUrl = copied ? undefined : url)} />
        </div>
      </div>
    </form>
    <Text as="div" size="sm" tone="muted"><bb-fit mode="block">{t('voice.overlayHint')}</bb-fit></Text>
    <div class="bb-collapse" class:is-open={!busy && !!status} aria-hidden={busy || !status} role="status">
      <div>
        <Text as="div" size="sm" tone="danger"><bb-fit mode="block">{status}</bb-fit></Text>
      </div>
    </div>
  </div>
</Card>

<style>
  .overlay,
  .overlay-form {
    --bb-collapse-gap: var(--bb-space-2);
    display: flex;
    flex-direction: column;
    gap: var(--bb-space-2);
  }
  .overlay-actions {
    display: grid;
    grid-template-columns: minmax(0, min(50%, 11rem));
    --btn-w: 100%;
    --btn-justify: center;
  }
  .overlay-action-label { line-height: 1.4; }
</style>
