<script lang="ts">
  import { page } from '$app/state';
  import { enhance } from '$app/forms';
  import { AppShell, ImpersonationBanner, NotificationBell, ToastHost } from '@bagel/shared';
  import type { NavGroupDef, NavLink } from '@bagel/shared';
  let { data, children } = $props();

  const isDelegate = $derived(!!data.delegateOf);

  let markReadForm = $state<HTMLFormElement | null>(null);
  let markReadId = $state<number | null>(null);
  function markRead(id: number) {
    markReadId = id;
    queueMicrotask(() => markReadForm?.requestSubmit());
  }

  const path = $derived(page.url.pathname);
  const crumb = $derived(
    path.startsWith('/commands')
      ? 'Commands'
      : path.startsWith('/modules')
        ? 'Modules'
        : path.startsWith('/billing')
          ? 'Billing'
          : path.startsWith('/settings') || path.startsWith('/access')
            ? 'Settings'
            : 'Overview'
  );

  // Delegate view: nav and routes are limited to the granted sections, and the
  // owner-only Overview/Settings entries are hidden.
  const sections = $derived((data.sections ?? []) as string[]);
  const canCommands = $derived(!isDelegate || sections.includes('commands'));
  const canModules = $derived(!isDelegate || sections.includes('modules'));

  // Notifications deliberately have NO nav entry: the topbar bell (badge +
  // dropdown, "View all" link) is the only way in.
  const items = $derived([
    ...(!isDelegate
      ? [{ href: '/', icon: 'overview', label: 'Overview', active: crumb === 'Overview' }]
      : []),
    ...(canCommands
      ? [{ href: '/commands', icon: 'commands', label: 'Commands', active: crumb === 'Commands' }]
      : []),
    ...(canModules
      ? [{ href: '/modules', icon: 'modules', label: 'Modules', active: crumb === 'Modules' }]
      : []),
    ...(!isDelegate
      ? [{ href: '/billing', icon: 'card', label: 'Billing', active: crumb === 'Billing' }]
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
  {#snippet topActions()}
    {#if !isDelegate}
      <NotificationBell
        notifications={(data.bellNotifications ?? [])}
        unreadCount={data.unreadCount ?? 0}
        viewAllHref="/settings#notifications"
        onMarkRead={markRead}
      />
    {/if}
  {/snippet}
  {@render children()}
</AppShell>

<!-- Hidden mark-read form the bell submits into; ?/markRead lives on the
     /settings route but SvelteKit actions can target any page. -->
<form method="POST" action="/settings?/markRead" use:enhance bind:this={markReadForm} hidden>
  <input type="hidden" name="id" value={markReadId ?? ''} />
</form>

<!-- One toast host for the whole app; pages push via the shared toast() store. -->
<ToastHost />
