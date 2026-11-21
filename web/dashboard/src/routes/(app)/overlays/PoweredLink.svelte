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

<Card as="section" class="dock dock-wide" aria-labelledby="powered-link">
  <div class="link-head">
    <Heading level={6} as="h2" id="powered-link">
      <bb-fit><span data-fit="">{t('overlays.linkTitle')}</span></bb-fit>
    </Heading>
    <form class="link-action" method="POST" action="?/rotate" use:enhance={submit}>
      <Button variant="secondary" size="sm" type="submit" {busy} disabled={busy} title={t('overlays.replaceHint')}>
        <bb-fit class="link-action-label"><span data-fit="">{t('overlays.replace')}</span></bb-fit>
      </Button>
    </form>
  </div>
  <div class="link-copy">
    <CopySurface variant="well" text={url} aria-label={t('overlays.link')}
      disabled={busy} legacyFallback onCopy={(copied) => (failedUrl = copied ? undefined : url)} />
  </div>
  <div class="link-feedback">
    <div class="feedback-text bb-fit-box" class:visible={busy || !status} aria-hidden={!busy && !!status}>
      <Text as="div" size="xs" tone="muted"><bb-fit>{t('overlays.obsHint')}</bb-fit></Text>
    </div>
    <div class="feedback-text bb-fit-box" class:visible={!busy && !!status} role="status">
      <Text as="div" size="xs" tone="danger"><bb-fit>{busy ? '' : status}</bb-fit></Text>
    </div>
  </div>
</Card>

<style>
  .link-head {
    display: grid;
    grid-template-columns: minmax(0, 1fr) 9rem;
    align-items: center;
    gap: var(--bb-space-3);
  }
  .link-action {
    --btn-w: 100%;
    --btn-justify: center;
  }
  .link-action-label { line-height: 1.4; }
  .link-copy { min-inline-size: 0; }
  .link-feedback {
    display: grid;
    grid-template-columns: minmax(0, 1fr);
  }
  .feedback-text {
    grid-area: 1 / 1;
    block-size: 1rem;
    min-inline-size: 0;
    align-content: center;
    white-space: nowrap;
    opacity: 0;
  }
  .feedback-text.visible { opacity: 1; }
  @media (prefers-reduced-motion: no-preference) {
    .feedback-text { transition: opacity var(--bb-dur-fast) var(--bb-ease-out-expo); }
  }
</style>
