<script lang="ts">
  import { page } from '$app/state';
  import { AppShell } from '@bagel/shared';
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

  const groups = $derived([
    {
      label: 'Operate',
      items: [
        { href: '/', icon: 'overview', label: 'Overview', active: crumb === 'Overview' },
        { href: '/shards', icon: 'pulse', label: 'Shards', active: crumb === 'Shards' },
        { href: '/lanes', icon: 'activity', label: 'Lanes', active: crumb === 'Lanes' }
      ]
    },
    {
      label: 'Accounts',
      items: [
        { href: '/users', icon: 'users', label: 'Users', active: crumb === 'Users' },
        { href: '/events', icon: 'bell', label: 'Events', active: crumb === 'Events' }
      ]
    },
    ...(isManager
      ? [
          {
            label: 'Access',
            items: [
              { href: '/staff', icon: 'moderation', label: 'Staff', active: crumb === 'Staff' },
              { href: '/audit', icon: 'check', label: 'Audit', active: crumb === 'Audit' },
              { href: '/credentials', icon: 'lock', label: 'Credentials', active: crumb === 'Credentials' }
            ]
          }
        ]
      : [])
  ] as NavGroupDef[]);

  // Condensed mobile nav: managers get staff/audit/creds, moderators get events.
  const mobileItems = $derived([
    { href: '/', icon: 'overview', label: 'Overview', active: crumb === 'Overview' },
    { href: '/shards', icon: 'pulse', label: 'Shards', active: crumb === 'Shards' },
    { href: '/lanes', icon: 'activity', label: 'Lanes', active: crumb === 'Lanes' },
    { href: '/users', icon: 'users', label: 'Users', active: crumb === 'Users' },
    ...(isManager
      ? [
          { href: '/staff', icon: 'moderation', label: 'Staff', active: crumb === 'Staff' },
          { href: '/audit', icon: 'check', label: 'Audit', active: crumb === 'Audit' },
          { href: '/credentials', icon: 'lock', label: 'Creds', active: crumb === 'Credentials' }
        ]
      : [{ href: '/events', icon: 'bell', label: 'Events', active: crumb === 'Events' }])
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
  {@render children()}
</AppShell>
