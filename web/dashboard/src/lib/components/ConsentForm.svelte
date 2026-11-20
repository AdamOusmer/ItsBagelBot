<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import { enhance, type ActionResult, type SubmitFunction } from '$app/forms';
  import { goto } from '$app/navigation';
  import { TERMS_VERSION } from '@bagel/kit/consent';
  import { translate, type Locale } from '@bagel/kit/i18n';
  import { SITE } from '@bagel/kit/site-links';
  import Button from '@bagel/ui/svelte/Button.svelte';
  import Checkbox from '@bagel/ui/svelte/Checkbox.svelte';
  import { toast } from '@bagel/ui/svelte/toast';

  type Props = {
    locale: Locale;
    requestId: string;
    mailingAddress: string;
    subscribed: boolean;
    maskedEmail: string;
    action?: string;
    serverError?: string;
    onaccepted?: () => void;
    accepted?: boolean;
    element?: HTMLFormElement | null;
  };

  let { locale, requestId, mailingAddress, subscribed, maskedEmail, action, serverError, onaccepted, accepted = $bindable(false), element = $bindable(null) }: Props = $props();
  const id = $props.id();
  const t = (key: string) => translate(locale, key);
  let busy = $state(false);
  let error = $state('');
  let retryId = $state('');

  const skipMessage = (reason: unknown) => {
    if (reason === 'no_email') return t('consent.marketingNoEmail');
    if (reason === 'unavailable') return t('consent.marketingUnavailable');
  };

  async function saved(data: Extract<ActionResult, { type: 'success' }>['data']) {
    const message = skipMessage(data?.marketingSkipped);
    if (typeof data?.next === 'string') await goto(data.next, { refreshAll: true });
    onaccepted?.();
    if (message) toast('info', message, { ttlMs: 8000 });
  }

  const submit: SubmitFunction = ({ cancel }) => {
    if (busy || !accepted) return cancel();
    busy = true;
    error = '';
    return async ({ result, update }) => {
      try {
        if (result.type === 'success') return await saved(result.data);
        if (result.type === 'redirect') return await update();
        retryId = crypto.randomUUID();
        error = result.type === 'failure' && typeof result.data?.error === 'string' ? result.data.error : t('consent.saveError');
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

  <div class="bb-setup-row choice terms">
    <Checkbox name="consent" value="yes" bind:checked={accepted} required disabled={busy} aria-labelledby="{id}-terms" />
    <div class="choice-copy">
      <bb-fit mode="block"><span data-fit id="{id}-terms" class="bb-prose bb-prose-inline">{@html t('onboarding.consentLabel')}</span></bb-fit>
    </div>
  </div>

  {#if subscribed}
    <div class="bb-setup-row choice terms">
      <div class="choice-copy"><bb-fit mode="block"><span data-fit>{t('consent.subscribed')}</span></bb-fit></div>
    </div>
  {:else if mailingAddress}
    <div class="bb-setup-row choice marketing">
      <Checkbox name="marketing" value="yes" disabled={busy} aria-labelledby="{id}-marketing" aria-describedby="{id}-details" />
      <div class="choice-copy" id="{id}-details">
        <bb-fit mode="block">
          <span data-fit id="{id}-marketing">{t('marketing.optInLabel')} {t('common.optional')}</span>
          <span data-fit class="marketing-detail">{translate(locale, 'marketing.optIn', { address: mailingAddress, contact: SITE.supportEmail })}</span>
          {#if maskedEmail}
            <span data-fit class="marketing-detail">{translate(locale, 'consent.email', { address: maskedEmail })}</span>
          {/if}
        </bb-fit>
      </div>
    </div>
  {/if}

  <div class="bb-setup-actions"><Button type="submit" disabled={!accepted} {busy}>{t('consent.continue')}</Button></div>
  <div class="status" role="status">{error || serverError || ''}</div>
</form>

<style>
  .consent-form { display: grid; gap: clamp(6px, 1.3svh, 12px); }
  .choice { align-items: center; padding-block: 8px; }
  .terms { block-size: 56px; }
  .marketing { block-size: clamp(108px, 21svh, 132px); }
  .choice-copy {
    align-content: center;
    block-size: 100%;
    flex: 1;
    min-width: 0;
    font-size: var(--bb-text-sm);
    line-height: 1.35;
    color: var(--bb-muted);
  }
  .marketing-detail { display: block; margin-top: 4px; }
  .status { block-size: 2.8em; color: var(--bb-status-danger); font-size: var(--bb-text-xs); text-align: center; }

  @media (max-height: 480px) {
    .choice { padding-block: 4px; }
    .terms { block-size: 40px; }
    .marketing { block-size: 84px; }
    .status { block-size: 1.4em; }
  }
</style>
