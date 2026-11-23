<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import { enhance, type ActionResult, type SubmitFunction } from '$app/forms';
  import { onMount } from 'svelte';
  import { TERMS_VERSION } from '@bagel/kit/consent';
  import { translate, type Locale } from '@bagel/kit/i18n';
  import Button from '@bagel/ui/svelte/Button.svelte';
  import Checkbox from '@bagel/ui/svelte/Checkbox.svelte';
  import { toast } from '@bagel/ui/svelte/toast';

  type Props = {
    locale: Locale;
    requestId: string;
    mailingAddress: string;
    subscribed: boolean;
    marketingDefault: boolean;
    action?: string;
    serverError?: string;
    onaccepted?: () => void;
    accepted?: boolean;
    element?: HTMLFormElement | null;
  };

  let { locale, requestId, mailingAddress, subscribed, marketingDefault, action, serverError, onaccepted, accepted = $bindable(false), element = $bindable(null) }: Props = $props();
  const t = (key: string) => translate(locale, key);
  let busy = $state(false);
  let error = $state('');
  let retryId = $state('');
  let mounted = $state(false);
  const status = $derived(error || serverError);

  onMount(() => { mounted = true; });

  function saved(data: Extract<ActionResult, { type: 'success' }>['data']) {
    onaccepted?.();
    const skipped = data?.marketingSkipped;
    if (skipped !== 'no_email' && skipped !== 'unavailable') return;
    const message = skipped === 'no_email' ? t('consent.marketingNoEmail') : t('consent.marketingUnavailable');
    toast('info', message, { ttlMs: 8000 });
  }

  const submit: SubmitFunction = ({ cancel }) => {
    if (busy || !accepted) return cancel();
    busy = true;
    error = '';
    return async ({ result, update }) => {
      try {
        if (result.type === 'success') return saved(result.data);
        if (result.type === 'redirect') return await update();
        retryId = crypto.randomUUID();
        const detail = result.type === 'failure' ? result.data?.error : undefined;
        error = typeof detail === 'string' ? detail : t('consent.saveError');
      } finally {
        busy = false;
      }
    };
  };
</script>

<form method="POST" {action} use:enhance={submit} bind:this={element} class="consent-form">
  <input type="hidden" name="request_id" value={retryId || requestId} />
  <input type="hidden" name="terms_version" value={TERMS_VERSION} />
  <input type="hidden" name="locale" value={locale} />

  <Checkbox class="bb-setup-row choice terms" name="consent" value="yes" bind:checked={accepted} required disabled={busy}>
    <bb-fit mode="block"><span class="bb-prose bb-prose-inline">{@html t('onboarding.consentLabel')}</span></bb-fit>
  </Checkbox>

  {#if subscribed}
    <div class="bb-setup-row choice terms">
      <div class="choice-copy"><bb-fit mode="block">{t('consent.subscribed')}</bb-fit></div>
    </div>
  {:else if mailingAddress}
    <Checkbox class="bb-setup-row choice marketing" name="marketing" value="yes" checked={marketingDefault} disabled={busy}>
      <bb-fit mode="block"><span>{t('marketing.optInLabel')}</span></bb-fit>
    </Checkbox>
  {/if}

  <div class="bb-setup-actions"><Button type="submit" disabled={mounted && !accepted} {busy}>{t('consent.continue')}</Button></div>
  <div class="status" role="status">{status ?? ''}</div>
</form>

<style>
  .consent-form { display: grid; gap: clamp(6px, 1.3svh, 12px); }
  .consent-form :global(.choice) { display: flex; align-items: center; gap: 12px; padding-block: 8px; font-size: var(--bb-text-sm); line-height: 1.35; color: var(--bb-muted); }
  .consent-form :global(.terms) { block-size: 56px; }
  .consent-form :global(.marketing) { block-size: 44px; font-size: var(--bb-text-xs); }
  .consent-form :global(.choice .bb-check__label) { flex: 1; min-width: 0; }
  .choice-copy {
    align-content: center;
    block-size: 100%;
    flex: 1;
    min-width: 0;
    font-size: var(--bb-text-sm);
    line-height: 1.35;
    color: var(--bb-muted);
  }
  .status { block-size: 2.8em; color: var(--bb-status-danger); font-size: var(--bb-text-xs); text-align: center; }

  @media (max-height: 480px) {
    .consent-form :global(.choice) { padding-block: 4px; }
    .consent-form :global(.terms) { block-size: 40px; }
    .consent-form :global(.marketing) { block-size: 32px; }
    .status { block-size: 1.4em; }
  }
</style>
