<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import '@bagel/kit/styles/ui-console.css';
  import { enhance, type SubmitFunction } from '$app/forms';
  import { getI18n } from '@bagel/kit/i18n/context';
  import { translate } from '@bagel/kit/i18n';
  import Brand from '@bagel/ui/svelte/Brand.svelte';
  import Button from '@bagel/ui/svelte/Button.svelte';
  import ButtonLink from '@bagel/ui/svelte/ButtonLink.svelte';
  import SetupStage from '@bagel/ui/svelte/SetupStage.svelte';
  import SetupStep from '@bagel/ui/svelte/SetupStep.svelte';

  const OPPOSITE = { granted: 'withdrawn', withdrawn: 'granted' } as const;

  let { data, form } = $props();
  const i18n = getI18n();
  const { t } = i18n;
  const title = $derived(t('emailPrefs.title'));
  const view = $derived(data.view);
  const bodies = $derived({
    granted: t('emailPrefs.granted'),
    withdrawn: t('emailPrefs.withdrawn'),
    none: t('emailPrefs.none'),
    invalid: t('emailPrefs.invalid'),
    unavailable: t('emailPrefs.unavailable')
  });
  const body = $derived(bodies[view.state === 'ready' ? view.status : view.state]);
  const choice = $derived(view.state === 'ready' && view.status !== 'none' ? OPPOSITE[view.status] : null);
  const status = $derived(form?.error ?? '');
  let busy = $state(false);

  const submit: SubmitFunction = () => {
    busy = true;
    return async ({ update }) => {
      await update();
      busy = false;
    };
  };
</script>

<svelte:head>
  <title>{title} · ItsBagelBot</title>
  <meta name="robots" content="noindex, nofollow" />
  <meta name="referrer" content="no-referrer" />
</svelte:head>

<SetupStage label={title} orbs={false}>
  {#snippet start()}<Brand title="ItsBagelBot" sub={t('common.console')} logoSrc="/logo.png" logoAlt="" size="md" />{/snippet}
  <SetupStep {title} {body} kicker="" instant>
    {#if view.state === 'ready'}
      {#if view.maskedEmail}
        <p class="address">{translate(i18n.locale, 'emailPrefs.email', { address: view.maskedEmail })}</p>
      {/if}
      {#if choice}
        <form method="POST" use:enhance={submit} class="bb-setup-actions">
          <input type="hidden" name="status" value={choice} />
          <Button type="submit" {busy}>{choice === 'withdrawn' ? t('emailPrefs.unsubscribe') : t('emailPrefs.resubscribe')}</Button>
        </form>
      {/if}
    {:else}
      <div class="bb-setup-actions"><ButtonLink href="/settings">{t('emailPrefs.settings')}</ButtonLink></div>
    {/if}
    <div class="status" role="status">{status}</div>
  </SetupStep>
</SetupStage>

<style>
  .address { margin: 0 0 12px; color: var(--bb-muted); font-size: var(--bb-text-sm); text-align: center; }
  .status { block-size: 2.8em; color: var(--bb-status-danger); font-size: var(--bb-text-xs); text-align: center; }
</style>
