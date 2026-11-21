<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  import { enhance, type SubmitFunction } from '$app/forms';
  import { getI18n } from '@bagel/kit/i18n/context';
  import Button from '@bagel/ui/svelte/Button.svelte';
  import Card from '@bagel/ui/svelte/Card.svelte';
  import CopySurface from '@bagel/ui/svelte/CopySurface.svelte';
  import Heading from '@bagel/ui/svelte/Heading.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';

  let { url, error, busy, submit }: {
    url: string;
    error?: string;
    busy: boolean;
    submit: SubmitFunction;
  } = $props();

  const { t } = getI18n();
  let failedUrl = $state<string>();
  const status = $derived(error ?? (failedUrl && failedUrl === url ? t('overlays.copyFailed') : ''));
</script>

<Card as="section" class="dock" aria-labelledby="powered-link">
  <Heading level={6} as="h2" id="powered-link">
    <bb-fit><span data-fit="">{t('overlays.linkTitle')}</span></bb-fit>
  </Heading>
  <div class="link-row">
    <div class="link-copy">
      <CopySurface variant="well" text={url} aria-label={t('overlays.link')}
        disabled={busy} legacyFallback onCopy={(copied) => (failedUrl = copied ? undefined : url)} />
    </div>
    <form class="link-action" method="POST" action="?/rotate" use:enhance={submit}>
      <Button variant="secondary" type="submit" {busy} disabled={busy} aria-describedby="powered-replace-hint">
        <bb-fit class="link-action-label"><span data-fit="">{t('overlays.replace')}</span></bb-fit>
      </Button>
    </form>
  </div>
  <div class="link-notes">
    <div class="link-feedback">
      <div class="feedback-text bb-fit-box" class:visible={busy || !status} aria-hidden={!busy && !!status}>
        <Text as="div" size="xs" tone="muted"><bb-fit mode="block">{t('overlays.obsHint')}</bb-fit></Text>
      </div>
      <div class="feedback-text bb-fit-box" class:visible={!busy && !!status} role="status">
        <Text as="div" size="xs" tone="danger"><bb-fit mode="block">{busy ? '' : status}</bb-fit></Text>
      </div>
    </div>
    <div class="note bb-fit-box" id="powered-replace-hint">
      <Text as="div" size="xs" tone="muted"><bb-fit mode="block">{t('overlays.replaceHint')}</bb-fit></Text>
    </div>
  </div>
</Card>

<style>
  .link-row {
    display: grid;
    grid-template-columns: minmax(0, 27.5rem) 11rem;
    justify-content: start;
    align-items: center;
    gap: var(--bb-space-3);
    margin-block-start: var(--bb-space-4);
  }
  .link-copy { min-inline-size: 0; }
  .link-action {
    --btn-w: 100%;
    --btn-justify: center;
  }
  .link-action-label { line-height: 1.4; }
  .link-notes {
    display: grid;
    grid-template-columns: minmax(0, 1fr);
    gap: var(--bb-space-1);
    margin-block-start: var(--bb-space-3);
  }
  .link-feedback {
    display: grid;
    grid-template-columns: minmax(0, 1fr);
  }
  .feedback-text, .note {
    block-size: 1.25rem;
    min-inline-size: 0;
    align-content: center;
    white-space: nowrap;
  }
  .feedback-text {
    grid-area: 1 / 1;
    opacity: 0;
  }
  .feedback-text.visible { opacity: 1; }
  @media (prefers-reduced-motion: no-preference) {
    .feedback-text { transition: opacity var(--bb-dur-fast) var(--bb-ease-out-expo); }
  }
  @media (max-width: 640px) {
    .link-row { grid-template-columns: minmax(0, 1fr); }
    .feedback-text, .note { block-size: 3em; line-height: 1.5; white-space: normal; align-content: start; }
  }
</style>
