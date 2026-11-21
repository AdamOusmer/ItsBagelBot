<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  import { enhance, type SubmitFunction } from '$app/forms';
  import { getI18n } from '@bagel/kit/i18n/context';
  import Button from '@bagel/ui/svelte/Button.svelte';
  import CopySurface from '@bagel/ui/svelte/CopySurface.svelte';
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

<div class="link">
  <form method="POST" action="?/rotate" use:enhance={submit}>
    <div class="link-actions">
      <div class="link-copy">
        <CopySurface variant="well" text={url} aria-label={t('overlays.link')}
          disabled={busy} legacyFallback onCopy={(copied) => (failedUrl = copied ? undefined : url)} />
      </div>
      <Button variant="secondary" size="sm" type="submit" {busy} disabled={busy}>
        <bb-fit class="link-action-label"><span data-fit="">{t('overlays.replace')}</span></bb-fit>
      </Button>
    </div>
  </form>
  <div class="link-feedback">
    <div class="feedback-text bb-fit-box" class:visible={busy || !status} aria-hidden={!busy && !!status}>
      <Text as="div" size="sm" tone="muted"><bb-fit mode="block">{t('overlays.obsHint')} {t('overlays.replaceHint')}</bb-fit></Text>
    </div>
    <div class="feedback-text bb-fit-box" class:visible={!busy && !!status} role="status">
      <Text as="div" size="sm" tone="danger"><bb-fit mode="block">{busy ? '' : status}</bb-fit></Text>
    </div>
  </div>
</div>

<style>
  .link {
    display: grid;
    grid-template-columns: minmax(0, 1fr);
    gap: var(--bb-space-2);
    padding: 14px 18px;
  }
  .link-actions {
    display: grid;
    grid-template-columns: minmax(0, 27.5rem) minmax(0, 11rem);
    justify-content: start;
    align-items: center;
    gap: var(--bb-space-2);
    --btn-w: 100%;
    --btn-justify: center;
  }
  .link-copy { min-inline-size: 0; }
  @media (max-width: 560px) {
    .link-actions { grid-template-columns: minmax(0, 1fr); }
  }
  .link-action-label { line-height: 1.4; }
  .link-feedback {
    display: grid;
    grid-template-columns: minmax(0, 1fr);
  }
  .feedback-text {
    grid-area: 1 / 1;
    block-size: 2.75rem;
    min-inline-size: 0;
    align-content: center;
    opacity: 0;
  }
  .feedback-text.visible { opacity: 1; }
  @media (prefers-reduced-motion: no-preference) {
    .feedback-text { transition: opacity var(--bb-dur-fast) var(--bb-ease-out-expo); }
  }
</style>
