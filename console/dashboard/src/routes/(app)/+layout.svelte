<script lang="ts">
  import { page } from '$app/state';
  import { enhance } from '$app/forms';
  import { AppShell, ImpersonationBanner, NotificationBell, ToastHost, getI18n } from '@bagel/shared';
  import type { NavGroupDef, NavLink } from '@bagel/shared';
  let { data, children } = $props();

  const { t } = getI18n();

  const isDelegate = $derived(!!data.delegateOf);

  let markReadForm = $state<HTMLFormElement | null>(null);
  let markReadId = $state<number | null>(null);
  function markRead(id: number) {
    markReadId = id;
    queueMicrotask(() => markReadForm?.requestSubmit());
  }

  // A stable section key drives active-state + the breadcrumb label; the label
  // itself is translated, so comparisons never break across languages.
  const path = $derived(page.url.pathname);
  const section = $derived(
    path.startsWith('/commands')
      ? 'commands'
      : path.startsWith('/modules')
        ? 'modules'
        : path.startsWith('/billing')
          ? 'billing'
          : path.startsWith('/settings') || path.startsWith('/access')
            ? 'settings'
            : 'overview'
  );
  const crumb = $derived(t(`nav.${section}`));

  // Delegate view: nav and routes are limited to the granted sections, and the
  // owner-only Overview/Settings entries are hidden.
  const sections = $derived((data.sections ?? []) as string[]);
  const canCommands = $derived(!isDelegate || sections.includes('commands'));
  const canModules = $derived(!isDelegate || sections.includes('modules'));

  // Notifications deliberately have NO nav entry: the topbar bell (badge +
  // dropdown, "View all" link) is the only way in.
  const items = $derived([
    ...(!isDelegate
      ? [{ href: '/', icon: 'overview', label: t('nav.overview'), active: section === 'overview' }]
      : []),
    ...(canCommands
      ? [{ href: '/commands', icon: 'commands', label: t('nav.commands'), active: section === 'commands' }]
      : []),
    ...(canModules
      ? [{ href: '/modules', icon: 'modules', label: t('nav.modules'), active: section === 'modules' }]
      : []),
    ...(!isDelegate
      ? [{ href: '/billing', icon: 'card', label: t('nav.billing'), active: section === 'billing' }]
      : []),
    ...(!isDelegate
      ? [{ href: '/settings', icon: 'settings', label: t('nav.settings'), active: section === 'settings' }]
      : [])
  ] as NavLink[]);

  const groups = $derived([{ label: t('nav.manage'), items }] as NavGroupDef[]);
  const showBanner = $derived(isDelegate || !!data.impersonatorLogin);
</script>

<AppShell
  brandSub={t('common.console')}
  crumbRoot="ItsBagelBot"
  {crumb}
  accountName={data.displayName}
  accountRole={t('topbar.roleBroadcaster')}
  {groups}
  mobileItems={items}
  offset={showBanner}
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
    {#if !isDelegate}
      <NotificationBell
        notifications={(data.bellNotifications ?? [])}
        unreadCount={data.unreadCount ?? 0}
        viewAllHref="/settings#notifications"
        onMarkRead={markRead}
        title={t('bell.title')}
        viewAllLabel={t('bell.viewAll')}
        emptyLabel={t('bell.empty')}
        readLabel={t('bell.read')}
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
