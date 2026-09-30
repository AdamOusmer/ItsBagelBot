<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import { page } from '$app/state';
  import { enhance } from '$app/forms';
  import type { SubmitFunction } from '@sveltejs/kit';
  import { toast } from '@bagel/ui/svelte/toast';
  import { onMount } from 'svelte';
  import { invalidateAll, afterNavigate } from '$app/navigation';
  import { visibleEventSource } from '$lib/visible-stream';
  import ConsoleShell from '@bagel/kit/components/ConsoleShell.svelte';
  import ImpersonationBanner from '@bagel/kit/components/ImpersonationBanner.svelte';
  import Icon from '@bagel/ui/svelte/Icon.svelte';
  import NotificationBell from '@bagel/kit/components/NotificationBell.svelte';
  import StatusDot from '@bagel/ui/svelte/StatusDot.svelte';
  import TextLink from '@bagel/ui/svelte/TextLink.svelte';
  import VisuallyHidden from '@bagel/ui/svelte/VisuallyHidden.svelte';
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
      es.onerror = () => (reconnecting = true);
      es.addEventListener('ready', () => {
        reconnecting = false;
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
  let reconnecting = $state(false);

  const failToast = (message: string): SubmitFunction => () => async ({ result, update }) => {
    if (result.type === 'failure' || result.type === 'error') toast('danger', message);
    await update({ reset: false });
  };

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

<ConsoleShell
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
  stacked={isDelegate && !!data.impersonatorLogin}
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
      <ImpersonationBanner exitForm second={isDelegate} exitLabel={t('banner.exit')}>
        {t('banner.viewingPre')}<b>{data.login}</b>{t('banner.viewingPost', { admin: data.impersonatorLogin })}
      </ImpersonationBanner>
    {/if}
  {/snippet}
  {#snippet topActions()}
    <span class="live-chip" class:live-chip--on={reconnecting} role="status" aria-live="polite">
      <StatusDot tone="warning" flat />
      <VisuallyHidden>{reconnecting ? t('topbar.reconnecting') : ''}</VisuallyHidden>
    </span>
    <span class="status-link">
      <TextLink variant="quiet" href="https://status.itsbagelbot.com" external>
        {t('nav.status')}<VisuallyHidden> {t('common.opensInNewTab')}</VisuallyHidden>
        <Icon name="arrowUpRight" size={11} strokeWidth={2} />
      </TextLink>
    </span>
    {#if !isDelegate}
      {#await data.bell}
        <span class="bell-slot" aria-hidden="true"></span>
      {:then bell}
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
</ConsoleShell>

<style>
  .bell-slot {
    width: 36px;
    height: 36px;
    flex: none;
    visibility: hidden;
  }
  .live-chip {
    display: inline-flex;
    align-items: center;
    flex: none;
    visibility: hidden;
    opacity: 0;
    transition: opacity var(--bb-dur-fast) var(--bb-ease-out-expo);
  }
  .live-chip--on {
    visibility: visible;
    opacity: 1;
  }
  .status-link {
    flex: none;
    display: none;
    white-space: nowrap;
  }
  @media (min-width: 761px) {
    .status-link { display: inline; }
  }
</style>

<form method="POST" action="/settings?/markRead" use:enhance={failToast(t('bell.markFailed'))} bind:this={markReadForm} hidden>
  <input type="hidden" name="id" value={markReadId ?? ''} />
</form>

<form method="POST" action="/settings?/markPeeked" use:enhance={failToast(t('serverErrors.updateRetry'))} bind:this={peekForm} hidden></form>

