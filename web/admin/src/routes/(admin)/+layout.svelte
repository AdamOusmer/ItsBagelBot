<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import { page } from '$app/state';
  import { afterNavigate } from '$app/navigation';
  import ConsoleShell from '@bagel/kit/components/ConsoleShell.svelte';
  import NotificationBell from '@bagel/kit/components/NotificationBell.svelte';
  import ToastHost from '@bagel/ui/svelte/ToastHost.svelte';
  import { getI18n } from '@bagel/kit/i18n/context';
  import type { NotificationWire } from '#lib/server/services';
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

  let liveCount = $state<number | null>(null);

  $effect(() => {
    const request = data.liveCount;
    liveCount = null;
    void request.then((count) => {
      if (request === data.liveCount) liveCount = count;
    });
  });

  const groups = $derived(adminNavGroups({ role, section, t, liveCount }));

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

{#snippet notificationBell(notifications: NotificationWire[], emptyLabel: string)}
  <NotificationBell
    {notifications}
    viewAllHref="/notifications"
    title={t('bell.title')}
    viewAllLabel={t('bell.viewAll')}
    {emptyLabel}
    unreadLabel={(count) => t('bell.unread', { count })}
    levelLabels={{
      info: t('bell.levelInfo'),
      success: t('bell.levelSuccess'),
      warning: t('bell.levelWarning'),
      critical: t('bell.levelCritical')
    }}
  />
{/snippet}

<ConsoleShell
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
      {@render notificationBell([], t('common.loading'))}
    {:then list}
      {@render notificationBell(list, t('bell.empty'))}
    {/await}
  {/snippet}
  {@render children()}
</ConsoleShell>

<ToastHost />
