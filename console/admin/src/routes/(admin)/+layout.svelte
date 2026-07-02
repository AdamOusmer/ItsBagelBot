<script lang="ts">
  import { page } from '$app/state';
  import { AppShell, NotificationBell, ToastHost } from '@bagel/shared';
  import type { NavGroupDef, NavLink } from '@bagel/shared';
  let { data, children } = $props();

  const path = $derived(page.url.pathname);
  const crumb = $derived(
    path.startsWith('/shards')
      ? 'Shards'
      : path.startsWith('/lanes')
        ? 'Lanes'
        : path.startsWith('/users')
          ? 'Users'
          : path.startsWith('/notifications')
            ? 'Notifications'
            : path.startsWith('/events')
              ? 'Events'
              : path.startsWith('/staff')
              ? 'Staff'
              : path.startsWith('/audit')
                ? 'Audit'
                : path.startsWith('/credentials')
                  ? 'Credentials'
                  : 'Overview'
  );

  // Staff roster + audit log + credentials are manager-only (admin/owner).
  // Moderators never see the nav entries; the routes also redirect them server-side.
  const isManager = $derived(data.role === 'admin' || data.role === 'owner');
  const roleLabel = $derived(
    data.role === 'owner' ? 'Owner' : data.role === 'admin' ? 'Admin' : 'Moderator'
  );

  // Notifications (compose + history) has NO nav entry: the topbar bell is the
  // only way in. Icons picked to read literally: tile grid, server racks,
  // road lanes, people, live pulse, shield, log document, lock.
  const groups = $derived([
    {
      label: 'Operate',
      items: [
        { href: '/', icon: 'overview', label: 'Overview', active: crumb === 'Overview' },
        { href: '/shards', icon: 'server', label: 'Shards', active: crumb === 'Shards' },
        { href: '/lanes', icon: 'lanes', label: 'Lanes', active: crumb === 'Lanes' }
      ]
    },
    {
      label: 'Accounts',
      items: [
        { href: '/users', icon: 'users', label: 'Users', active: crumb === 'Users' },
        { href: '/events', icon: 'pulse', label: 'Events', active: crumb === 'Events' }
      ]
    },
    ...(isManager
      ? [
          {
            label: 'Access',
            items: [
              { href: '/staff', icon: 'moderation', label: 'Staff', active: crumb === 'Staff' },
              { href: '/audit', icon: 'audit', label: 'Audit', active: crumb === 'Audit' },
              { href: '/credentials', icon: 'lock', label: 'Credentials', active: crumb === 'Credentials' }
            ]
          }
        ]
      : [])
  ] as NavGroupDef[]);

  // Dock items (the only navigation now): everyone gets the operate/accounts
  // set; managers additionally get staff/audit/creds.
  const mobileItems = $derived([
    { href: '/', icon: 'overview', label: 'Overview', active: crumb === 'Overview' },
    { href: '/shards', icon: 'server', label: 'Shards', active: crumb === 'Shards' },
    { href: '/lanes', icon: 'lanes', label: 'Lanes', active: crumb === 'Lanes' },
    { href: '/users', icon: 'users', label: 'Users', active: crumb === 'Users' },
    { href: '/events', icon: 'pulse', label: 'Events', active: crumb === 'Events' },
    ...(isManager
      ? [
          { href: '/staff', icon: 'moderation', label: 'Staff', active: crumb === 'Staff' },
          { href: '/audit', icon: 'audit', label: 'Audit', active: crumb === 'Audit' },
          { href: '/credentials', icon: 'lock', label: 'Creds', active: crumb === 'Credentials' }
        ]
      : [])
  ] as NavLink[]);
</script>

<AppShell
  brandSub="Admin"
  crumbRoot="Admin"
  {crumb}
  accountName={data.displayName}
  accountRole={roleLabel}
  {groups}
  {mobileItems}
>
  {#snippet topActions()}
    <NotificationBell
      notifications={(data.recentNotifications ?? [])}
      viewAllHref="/notifications"
      emptyLabel="Nothing sent yet."
    />
  {/snippet}
  {@render children()}
</AppShell>

<!-- One toast host for the whole admin app; pages push via the shared toast() store. -->
<ToastHost />
