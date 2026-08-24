<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import { page } from '$app/state';
  import { enhance } from '$app/forms';
  import { onMount } from 'svelte';
  import { invalidateAll, afterNavigate } from '$app/navigation';
  import {
    AppShell,
    ImpersonationBanner,
    NotificationBell,
    ToastHost,
    getI18n,
    sectionForPath,
    dashboardNavItems,
    dashboardNavGroups
  } from '@bagel/shared';
  let { data, children } = $props();

  const i18n = getI18n();
  const { t } = i18n;

  // Live refresh: one EventSource to /events, fed by the same cache-invalidation
  // bus every Go write publishes. On any event for this user's board — and on
  // every (re)connect, to reconcile anything missed while briefly offline — we
  // re-fetch, so an open page (e.g. billing flipping to premium after a payment
  // webhook) updates on its own with no polling. Delegates get no /events (the
  // stream is owner/board-scoped and delegate pages already SSR fresh).
  onMount(() => {
    if (typeof EventSource === 'undefined' || isDelegate) return;
    let debounce: ReturnType<typeof setTimeout> | undefined;
    let seenReady = false;
    const refresh = () => {
      clearTimeout(debounce);
      debounce = setTimeout(() => void invalidateAll(), 250);
    };
    const es = new EventSource('/events');
    es.addEventListener('invalidate', refresh);
    // The first 'ready' is the initial connect (the page already SSR'd fresh);
    // only reconcile on later ones (reconnects after a drop).
    es.addEventListener('ready', () => {
      if (seenReady) refresh();
      else seenReady = true;
    });
    return () => {
      clearTimeout(debounce);
      es.close();
    };
  });

  const isDelegate = $derived(!!data.delegateOf);

  let markReadForm = $state<HTMLFormElement | null>(null);
  let markReadId = $state<number | null>(null);
  function markRead(id: number) {
    markReadId = id;
    queueMicrotask(() => markReadForm?.requestSubmit());
  }

  // Opening the bell dropdown soft-acknowledges everything (server-side "peek");
  // fired once per page by the bell so it only round-trips on the first open.
  let peekForm = $state<HTMLFormElement | null>(null);
  function peek() {
    queueMicrotask(() => peekForm?.requestSubmit());
  }

  // A stable section key drives active-state + the breadcrumb label; the label
  // itself is translated, so comparisons never break across languages. The
  // path→section ladder lives in the shared nav registry (DASHBOARD_SECTIONS),
  // alongside everything else that needs to know a page's section.
  const path = $derived(page.url.pathname);
  const section = $derived(sectionForPath(path));
  const crumb = $derived(t(`nav.${section}`));

  // A client route change (unlike a full load) neither moves focus nor updates
  // the title on its own. Keep the title in sync every time; move focus only on
  // real navigations (skip the initial SSR hydration, type 'enter', which is
  // already parked correctly). A hash targets a section heading; otherwise the
  // page <h1> (tabindex=-1, so no persistent ring appears).
  afterNavigate(({ type }) => {
    document.title = `${crumb} · ItsBagelBot`;
    if (type === 'enter') return;
    const id = page.url.hash.slice(1);
    // Wait a frame so the new page has rendered before reaching for its heading.
    requestAnimationFrame(() => {
      const target = id ? document.getElementById(id) : null;
      if (target) {
        target.focus();
        if (document.activeElement === target) return;
      }
      document.querySelector<HTMLElement>('#main-content h1')?.focus();
    });
  });

  // app.html hard-codes lang="en"; mirror the live locale onto <html lang> so a
  // language switch is reflected client-side (the SSR side is the integrator's).
  $effect(() => {
    document.documentElement.lang = i18n.locale;
  });

  // Delegate view: nav and routes are limited to the granted sections, and the
  // owner-only Overview/Settings entries are hidden (visibility rules live on
  // the registry's ownerOnly/grant flags). Billing is owner-only except for a
  // delegate explicitly granted it (view-only).
  const sections = $derived((data.sections ?? []) as string[]);

  // Notifications deliberately have NO nav entry: the topbar bell (badge +
  // dropdown, "View all" link) is the only way in. path/hash feed the
  // subsection active flags (module categories are hash-gated to the hub).
  const items = $derived(
    dashboardNavItems({ isDelegate, sections, section, path, hash: page.url.hash, t })
  );
  const groups = $derived(dashboardNavGroups(items, t));
  const showBanner = $derived(isDelegate || !!data.impersonatorLogin);
</script>

<!-- Authed app surface: never index a signed-in user's board (defense-in-depth
     on top of robots.txt, which already disallows these paths). -->
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
      <NotificationBell
        notifications={(data.bellNotifications ?? [])}
        unreadCount={data.unreadCount ?? 0}
        viewAllHref="/settings#notifications"
        onMarkRead={markRead}
        onOpen={peek}
        title={t('bell.title')}
        viewAllLabel={t('bell.viewAll')}
        emptyLabel={t('bell.empty')}
        readLabel={t('bell.read')}
      />
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
  }
  .status-link:hover {
    color: var(--bb-tan-pale, #eceae1);
  }
</style>

<!-- Hidden mark-read form the bell submits into; ?/markRead lives on the
     /settings route but SvelteKit actions can target any page. -->
<form method="POST" action="/settings?/markRead" use:enhance bind:this={markReadForm} hidden>
  <input type="hidden" name="id" value={markReadId ?? ''} />
</form>

<!-- Hidden peek form: submitted once when the bell dropdown first opens. -->
<form method="POST" action="/settings?/markPeeked" use:enhance bind:this={peekForm} hidden></form>

<!-- One toast host for the whole app; pages push via the shared toast() store. -->
<ToastHost />
