<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import ErrorScene from '@bagel/ui/svelte/ErrorScene.svelte';
  import { page } from '$app/state';
  import { getI18n } from '../lib/i18n/context';

  let { appName, loginHref = '/login' }: { appName: string; loginHref?: string } = $props();
  const { t } = getI18n();
  const localizedApp = $derived(appName === 'Dashboard' ? t('error.appDashboard') : appName === 'Admin' ? t('error.appAdmin') : appName);
  const homeLabel = $derived(appName === 'Dashboard' ? t('error.backToDashboard') : t('error.backToApp', { app: localizedApp }));

  function retry() {
    window.location.reload();
  }

  function goBack() {
    if (window.history.length > 1) window.history.back();
    else window.location.assign('/');
  }

  const retryAction = { label: t('error.tryAgain'), onclick: retry };
  const backAction = { label: t('error.goBack'), onclick: goBack };

  const view = $derived.by(() => {
    if (page.status === 404) return {
      eyebrow: t('error.notFoundEyebrow'),
      title: t('error.notFoundTitle'),
      description: t('error.notFoundDescription'),
      primary: { label: homeLabel, href: '/' }
    };
    if (page.status === 401 || page.status === 403) return {
      eyebrow: t('error.accessEyebrow'),
      title: t('error.accessTitle'),
      description: t('error.accessDescription', { app: localizedApp }),
      primary: { label: t('error.signIn'), href: loginHref }
    };
    if (page.status === 500 || page.status === 503) return {
      eyebrow: t('error.serverEyebrow'),
      title: t('error.serverTitle'),
      description: t('error.serverDescription'),
      primary: retryAction
    };
    return {
      eyebrow: t('error.unexpectedEyebrow'),
      title: t('error.unexpectedTitle'),
      description: page.error?.message ?? t('error.unexpectedDescription'),
      primary: retryAction
    };
  });
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
  primary={view.primary}
  secondary={backAction}
/>
