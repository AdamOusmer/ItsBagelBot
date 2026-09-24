<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import { page } from '$app/state';
  import { afterNavigate } from '$app/navigation';
  import AppShell from '@bagel/kit/components/AppShell.svelte';
  import NotificationBell from '@bagel/ui/svelte/NotificationBell.svelte';
  import ToastHost from '@bagel/ui/svelte/ToastHost.svelte';
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

  const path = $derived(page.url.pathname);
  const section = $derived(adminSectionForPath(path));
  const crumb = $derived(t(adminSectionLabelKey(section)));

  const role = $derived((data.role ?? 'moderator') as StaffRole);

  const ROLE_LABEL: Record<StaffRole, Parameters<typeof t>[0]> = {
    moderator: 'admin.roleModerator',
    admin: 'admin.roleAdmin',
    owner: 'admin.roleOwner'
  };
  const roleLabel = $derived(t(ROLE_LABEL[role]));

  const groups = $derived(adminNavGroups({ role, section, t }));

  afterNavigate(({ type }) => {
    document.title = `${crumb} · ItsBagelBot Admin`;
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
</script>

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

<ToastHost />
