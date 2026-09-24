<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import { page } from '$app/state';
  import { enhance } from '$app/forms';
  import { onMount } from 'svelte';
  import { invalidateAll, afterNavigate } from '$app/navigation';
  import { visibleEventSource } from '$lib/visible-stream';
  import AppShell from '@bagel/kit/components/AppShell.svelte';
  import ImpersonationBanner from '@bagel/kit/components/ImpersonationBanner.svelte';
  import NotificationBell from '@bagel/ui/svelte/NotificationBell.svelte';
  import ToastHost from '@bagel/ui/svelte/ToastHost.svelte';
  import { getI18n } from '@bagel/kit/i18n/context';
  import { sectionForPath, dashboardNavItems, dashboardNavGroups } from '@bagel/kit/nav-dashboard';
  let { data, children } = $props();

  const i18n = getI18n();
  const { t } = i18n;

  onMount(() => {
    if (typeof EventSource === 'undefined' || isDelegate) return;
    let debounce: ReturnType<typeof setTimeout> | undefined;
    let seenReady = false;
    const refresh = () => {
      clearTimeout(debounce);
      debounce = setTimeout(() => void invalidateAll(), 250);
    };
    const stop = visibleEventSource('/events', (es) => {
      es.addEventListener('invalidate', refresh);
      es.addEventListener('ready', () => {
        if (seenReady) refresh();
        else seenReady = true;
      });
    });
    return () => {
      clearTimeout(debounce);
      stop();
    };
  });

  const isDelegate = $derived(!!data.delegateOf);

  let markReadForm = $state<HTMLFormElement | null>(null);
  let markReadId = $state<number | null>(null);
  function markRead(id: number) {
    markReadId = id;
    queueMicrotask(() => markReadForm?.requestSubmit());
  }

  let peekForm = $state<HTMLFormElement | null>(null);
  function peek() {
    queueMicrotask(() => peekForm?.requestSubmit());
  }

  const path = $derived(page.url.pathname);
  const section = $derived(sectionForPath(path));
  const crumb = $derived(t(`nav.${section}`));

  afterNavigate(({ type }) => {
    document.title = `${crumb} · ItsBagelBot`;
    if (type === 'enter') return;
    const id = page.url.hash.slice(1);
    requestAnimationFrame(() => {
      const target = id ? document.getElementById(id) : null;
      if (target) {
        target.focus();
        if (document.activeElement === target) return;
      }
      document.querySelector<HTMLElement>('#main-content h1')?.focus();
    });
  });

  $effect(() => {
    document.documentElement.lang = i18n.locale;
  });

  const sections = $derived((data.sections ?? []) as string[]);

  const items = $derived(dashboardNavItems({
    isDelegate, sections, section, t,
    moduleLinks: data.moduleNav.map((link) => ({ ...link, label: t(link.label) }))
  }));
  const groups = $derived(dashboardNavGroups(items, t));
  const showBanner = $derived(isDelegate || !!data.impersonatorLogin);
</script>

<svelte:head>
  <meta name="robots" content="noindex, nofollow" />
</svelte:head>

<AppShell
  brandSub={t('common.console')}
  crumbRoot="ItsBagelBot"
  {crumb}
  accountName={data.displayName}
  accountRole={t('topbar.roleBroadcaster')}
  dashboards={data.authorizedDashboards ?? []}
  {groups}
  mobileItems={items}
  rail
  offset={showBanner}
  logoSrc={data.isPremium ? '/premium-logo.png' : '/logo.png'}
  isPremium={data.isPremium}
  {isDelegate}
  delegateExitHref="/delegate/exit"
  delegateExitLabel={t('banner.exit')}
>
  {#snippet banner()}
    {#if isDelegate}
      <ImpersonationBanner exitHref="/delegate/exit" exitLabel={t('banner.exit')}>
        {t('banner.sharedPre')}<b>{data.delegateLogin}</b>{t('banner.sharedPost', { sections: sections.join(', ') })}
      </ImpersonationBanner>
    {/if}
    {#if data.impersonatorLogin}
      <ImpersonationBanner exitForm exitLabel={t('banner.exit')}>
        {t('banner.viewingPre')}<b>{data.login}</b>{t('banner.viewingPost', { admin: data.impersonatorLogin })}
      </ImpersonationBanner>
    {/if}
  {/snippet}
  {#snippet topActions()}
    <a href="https://status.itsbagelbot.com" class="status-link" target="_blank" rel="noopener noreferrer">{t('nav.status')}</a>
    {#if !isDelegate}
      {#await data.bell then bell}
        <NotificationBell
          notifications={bell.notifications}
          unreadCount={bell.unreadCount}
          viewAllHref="/settings#notifications"
          onMarkRead={markRead}
          onOpen={peek}
          title={t('bell.title')}
          viewAllLabel={t('bell.viewAll')}
          emptyLabel={t('bell.empty')}
          readLabel={t('bell.read')}
        />
      {/await}
    {/if}
  {/snippet}
  {@render children()}
</AppShell>

<style>
  .status-link {
    font-family: var(--bb-font-body, inherit);
    font-size: 13px;
    font-weight: 500;
    color: var(--bb-muted, #a39b8b);
    text-decoration: none;
    transition: color 180ms ease;
    white-space: nowrap;
    flex: none;
    display: none;
  }
  @media (min-width: 761px) {
    .status-link { display: inline; }
  }
  .status-link:hover {
    color: var(--bb-tan-pale, #eceae1);
  }
</style>

<form method="POST" action="/settings?/markRead" use:enhance bind:this={markReadForm} hidden>
  <input type="hidden" name="id" value={markReadId ?? ''} />
</form>

<form method="POST" action="/settings?/markPeeked" use:enhance bind:this={peekForm} hidden></form>

<ToastHost />
