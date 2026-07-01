<script lang="ts">
  import { page } from '$app/state';
  import { AppShell, ImpersonationBanner, ToastHost } from '@bagel/shared';
  import type { NavGroupDef, NavLink } from '@bagel/shared';
  let { data, children } = $props();

  const path = $derived(page.url.pathname);
  const crumb = $derived(
    path.startsWith('/commands')
      ? 'Commands'
      : path.startsWith('/modules')
        ? 'Modules'
        : path.startsWith('/settings') || path.startsWith('/access')
          ? 'Settings'
          : 'Overview'
  );

  // Delegate view: nav and routes are limited to the granted sections, and the
  // owner-only Overview/Settings entries are hidden.
  const isDelegate = $derived(!!data.delegateOf);
  const sections = $derived((data.sections ?? []) as string[]);
  const canCommands = $derived(!isDelegate || sections.includes('commands'));
  const canModules = $derived(!isDelegate || sections.includes('modules'));

  const items = $derived([
    ...(!isDelegate
      ? [{ href: '/', icon: 'overview', label: 'Overview', active: crumb === 'Overview' }]
      : []),
    ...(canCommands
      ? [{ href: '/commands', icon: 'commands', label: 'Commands', active: crumb === 'Commands' }]
      : []),
    ...(canModules
      ? [{ href: '/modules', icon: 'power', label: 'Modules', active: crumb === 'Modules' }]
      : []),
    ...(!isDelegate
      ? [{ href: '/settings', icon: 'settings', label: 'Settings', active: crumb === 'Settings' }]
      : [])
  ] as NavLink[]);

  const groups = $derived([{ label: 'Manage', items }] as NavGroupDef[]);
  const showBanner = $derived(isDelegate || !!data.impersonatorLogin);
</script>

<AppShell
  brandSub="Console"
  crumbRoot="ItsBagelBot"
  {crumb}
  accountName={data.displayName}
  accountRole="Broadcaster"
  {groups}
  mobileItems={items}
  offset={showBanner}
>
  {#snippet banner()}
    {#if isDelegate}
      <ImpersonationBanner exitHref="/delegate/exit">
        Shared access to <b>{data.delegateLogin}</b>'s dashboard ({sections.join(', ')})
      </ImpersonationBanner>
    {/if}
    {#if data.impersonatorLogin}
      <ImpersonationBanner exitForm>
        Viewing as <b>{data.login}</b> (admin {data.impersonatorLogin})
      </ImpersonationBanner>
    {/if}
  {/snippet}
  {@render children()}
</AppShell>

<!-- One toast host for the whole app; pages push via the shared toast() store. -->
<ToastHost />
