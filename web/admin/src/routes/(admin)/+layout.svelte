<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import { page } from '$app/state';
  import { afterNavigate } from '$app/navigation';
  // Direct imports, not the barrel: this layout is on every operator page's
  // boot path (see routes/+layout.svelte).
  import AppShell from '@bagel/kit/components/AppShell.svelte';
  import NotificationBell from '@bagel/kit/components/NotificationBell.svelte';
  import ToastHost from '@bagel/kit/components/ToastHost.svelte';
  import { getI18n } from '@bagel/kit/i18n/context';
  import {
    adminNavGroups,
    adminSectionForPath,
    adminSectionLabelKey
  } from '@bagel/kit/nav-admin';
  import type { StaffRole } from '@bagel/kit/staff-role';
  let { data, children } = $props();

  const i18n = getI18n();
  const { t } = i18n;

  // A stable section key drives active-state + the breadcrumb label; the label
  // itself is translated, so comparisons never break across languages. The
  // path -> section ladder, the group each section belongs to and the least
  // role that may see it all live in the shared registry (ADMIN_SECTIONS),
  // which replaced the three hand-kept literals this file used to carry.
  const path = $derived(page.url.pathname);
  const section = $derived(adminSectionForPath(path));
  const crumb = $derived(t(adminSectionLabelKey(section)));

  const role = $derived((data.role ?? 'moderator') as StaffRole);

  // Flat table, not a ternary chain: three roles today, and a fourth would
  // otherwise nest.
  // Keyed off t's own parameter type so a renamed key fails here, not silently
  // at runtime; '@bagel/kit/i18n/keys' is a .d.ts with no export subpath.
  const ROLE_LABEL: Record<StaffRole, Parameters<typeof t>[0]> = {
    moderator: 'admin.roleModerator',
    admin: 'admin.roleAdmin',
    owner: 'admin.roleOwner'
  };
  const roleLabel = $derived(t(ROLE_LABEL[role]));

  // The rail and the dock read the same groups. mobileItems stays empty on
  // purpose: with more than one group the Dock switches to its grouped mode
  // (one button + popover per group), which is the only shape that survives ten
  // sections on a phone -- the flat list this file used to pass was dead code
  // in that mode anyway.
  const groups = $derived(adminNavGroups({ role, section, t }));

  // A client route change (unlike a full load) neither moves focus nor updates
  // the title on its own. Keep the title in sync every time; move focus only on
  // real navigations (skip the initial SSR hydration, type 'enter', which is
  // already parked correctly). A hash targets a section heading; otherwise the
  // page <h1> (tabindex=-1, so no persistent ring appears).
  //
  // No live-refresh EventSource here, unlike the dashboard: the dashboard's
  // /events is a per-board cache-invalidation stream, and this console has no
  // equivalent. Its only SSE is /events/stream, the ingress shard-lifecycle
  // firehose the Ingress feed page already consumes; re-fetching the board on
  // every message on it would be a refresh storm, not a live view.
  afterNavigate(({ type }) => {
    document.title = `${crumb} · ItsBagelBot Admin`;
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
</script>

<!-- Operator surface: never index it (defense in depth on top of the tailnet
     and robots.txt, which already disallow these paths). -->
<svelte:head>
  <meta name="robots" content="noindex, nofollow" />
</svelte:head>

<AppShell
  brandSub={t('admin.title')}
  crumbRoot={t('admin.title')}
  {crumb}
  accountName={data.displayName}
  accountRole={roleLabel}
  {groups}
  mobileItems={[]}
  rail
>
  {#snippet topActions()}
    <!-- Bell data is streamed; render the bell immediately with an honest
         empty state and hydrate when the peek lands. -->
    {#await data.recentNotifications}
      <NotificationBell
        notifications={[]}
        viewAllHref="/notifications"
        title={t('bell.title')}
        viewAllLabel={t('bell.viewAll')}
        emptyLabel={t('common.loading')}
      />
    {:then list}
      <NotificationBell
        notifications={list}
        viewAllHref="/notifications"
        title={t('bell.title')}
        viewAllLabel={t('bell.viewAll')}
        emptyLabel={t('bell.empty')}
      />
    {/await}
  {/snippet}
  {@render children()}
</AppShell>

<!-- One toast host for the whole admin app; pages push via the shared toast() store. -->
<ToastHost />
