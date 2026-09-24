<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import ErrorScene from '@bagel/ui/svelte/ErrorScene.svelte';
  import { page } from '$app/state';
  import { getI18n } from '../lib/i18n/context';

  let { appName, loginHref = '/login' }: { appName: string; loginHref?: string } = $props();
  const { t } = getI18n();
  const localizedApp = $derived(appName === 'Dashboard' ? t('error.appDashboard') : appName === 'Admin' ? t('error.appAdmin') : appName);

  const view = $derived.by(() => {
    if (page.status === 404) return {
      eyebrow: t('error.notFoundEyebrow'),
      title: t('error.notFoundTitle'),
      description: t('error.notFoundDescription'),
      action: 'home' as const
    };
    if (page.status === 401 || page.status === 403) return {
      eyebrow: t('error.accessEyebrow'),
      title: t('error.accessTitle'),
      description: t('error.accessDescription', { app: localizedApp }),
      action: 'login' as const
    };
    if (page.status === 500 || page.status === 503) return {
      eyebrow: t('error.serverEyebrow'),
      title: t('error.serverTitle'),
      description: t('error.serverDescription'),
      action: 'retry' as const
    };
    return {
      eyebrow: t('error.unexpectedEyebrow'),
      title: t('error.unexpectedTitle'),
      description: page.error?.message ?? t('error.unexpectedDescription'),
      action: 'retry' as const
    };
  });

  const homeLabel = $derived(appName === 'Dashboard' ? t('error.backToDashboard') : t('error.backToApp', { app: localizedApp }));

  function retry() {
    window.location.reload();
  }

  function goBack() {
    if (window.history.length > 1) window.history.back();
    else window.location.assign('/');
  }
</script>

<svelte:head>
  <title>{page.status}: ItsBagelBot {localizedApp}</title>
</svelte:head>

<ErrorScene
  status={page.status}
  eyebrow={view.eyebrow}
  title={view.title}
  description={view.description}
  aside={t('error.aside')}
  labelledBy="error-title"
>
  {#snippet actions()}
    {#if view.action === 'retry'}
      <button class="bb-error-scene__action bb-error-scene__action--primary" type="button" onclick={retry}>{t('error.tryAgain')}</button>
    {:else if view.action === 'login'}
      <a class="bb-error-scene__action bb-error-scene__action--primary" href={loginHref}>{t('error.signIn')}</a>
    {:else}
      <a class="bb-error-scene__action bb-error-scene__action--primary" href="/">{homeLabel}</a>
    {/if}
    <button class="bb-error-scene__action bb-error-scene__action--quiet" type="button" onclick={goBack}>{t('error.goBack')}</button>
  {/snippet}
</ErrorScene>
