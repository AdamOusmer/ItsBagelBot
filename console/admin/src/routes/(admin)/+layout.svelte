<script lang="ts">
  import { page } from '$app/state';
  import { AppShell, NotificationBell, ToastHost } from '@bagel/shared';
  import type { NavGroupDef, NavLink } from '@bagel/shared';
  let { data, children } = $props();

  const path = $derived(page.url.pathname);

  // First path segment -> breadcrumb. Data-driven so adding a page is one row.
  const CRUMBS: [prefix: string, label: string][] = [
    ['/shards', 'Shards'],
    ['/lanes', 'Lanes'],
    ['/events', 'Events'],
    ['/users', 'Users'],
    ['/notifications', 'Notifications'],
    ['/staff', 'Staff'],
    ['/audit', 'Audit'],
    ['/secrets', 'Secrets']
  ];
  const crumb = $derived(CRUMBS.find(([p]) => path.startsWith(p))?.[1] ?? 'Overview');

  // Staff roster + audit log + secrets are manager-only (admin/owner).
  // Moderators never see the nav entries; the routes also redirect them server-side.
  const isManager = $derived(data.role === 'admin' || data.role === 'owner');
  const roleLabel = $derived(
    data.role === 'owner' ? 'Owner' : data.role === 'admin' ? 'Admin' : 'Moderator'
  );

  // Notifications (compose + history) has NO nav entry: the topbar bell is the
  // only way in.
  const groups = $derived([
    {
      label: 'Operate',
      items: [
        { href: '/', icon: 'overview', label: 'Overview', active: crumb === 'Overview' },
        { href: '/shards', icon: 'server', label: 'Shards', active: crumb === 'Shards' },
        { href: '/lanes', icon: 'lanes', label: 'Lanes', active: crumb === 'Lanes' },
        { href: '/events', icon: 'pulse', label: 'Events', active: crumb === 'Events' }
      ]
    },
    {
      label: 'Accounts',
      items: [{ href: '/users', icon: 'users', label: 'Users', active: crumb === 'Users' }]
    },
    ...(isManager
      ? [
          {
            label: 'Access',
            items: [
              { href: '/staff', icon: 'moderation', label: 'Staff', active: crumb === 'Staff' },
              { href: '/audit', icon: 'audit', label: 'Audit', active: crumb === 'Audit' },
              { href: '/secrets', icon: 'lock', label: 'Secrets', active: crumb === 'Secrets' }
            ]
          }
        ]
      : [])
  ] as NavGroupDef[]);

  // Dock items (mobile): everyone gets the operate/accounts set; managers
  // additionally get staff/audit/secrets.
  const mobileItems = $derived([
    { href: '/', icon: 'overview', label: 'Overview', active: crumb === 'Overview' },
    { href: '/shards', icon: 'server', label: 'Shards', active: crumb === 'Shards' },
    { href: '/lanes', icon: 'lanes', label: 'Lanes', active: crumb === 'Lanes' },
    { href: '/events', icon: 'pulse', label: 'Events', active: crumb === 'Events' },
    { href: '/users', icon: 'users', label: 'Users', active: crumb === 'Users' },
    ...(isManager
      ? [
          { href: '/staff', icon: 'moderation', label: 'Staff', active: crumb === 'Staff' },
          { href: '/audit', icon: 'audit', label: 'Audit', active: crumb === 'Audit' },
          { href: '/secrets', icon: 'lock', label: 'Secrets', active: crumb === 'Secrets' }
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
    <!-- Bell data is streamed; render the bell immediately with an honest
         empty state and hydrate when the peek lands. -->
    {#await data.recentNotifications}
      <NotificationBell notifications={[]} viewAllHref="/notifications" emptyLabel="Loading…" />
    {:then list}
      <NotificationBell
        notifications={list}
        viewAllHref="/notifications"
        emptyLabel="Nothing sent yet."
      />
    {/await}
  {/snippet}
  {@render children()}
</AppShell>

<!-- One toast host for the whole admin app; pages push via the shared toast() store. -->
<ToastHost />
