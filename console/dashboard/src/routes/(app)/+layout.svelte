<script lang="ts">
  import { page } from '$app/state';
  import { enhance } from '$app/forms';
  import { onMount } from 'svelte';
  import { invalidateAll } from '$app/navigation';
  import { AppShell, ImpersonationBanner, NotificationBell, ToastHost, getI18n } from '@bagel/shared';
  import type { NavGroupDef, NavLink } from '@bagel/shared';
  let { data, children } = $props();

  const { t } = getI18n();

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
  // itself is translated, so comparisons never break across languages.
  const path = $derived(page.url.pathname);
  const section = $derived(
    path.startsWith('/commands')
      ? 'commands'
      : path.startsWith('/modules') || path.startsWith('/govee')
        ? 'modules'
        : path.startsWith('/channelpoints')
          ? 'channelpoints'
          : path.startsWith('/timers')
            ? 'timers'
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
  const canChannelPoints = $derived(!isDelegate || sections.includes('channelpoints'));
  // Timers rides under the commands scope; legacy 'timers' grants still count.
  const canTimers = $derived(!isDelegate || sections.includes('commands') || sections.includes('timers'));
  // Billing is owner-only except for a delegate explicitly granted it (view-only).
  const canBilling = $derived(!isDelegate || sections.includes('billing'));

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
    ...(canChannelPoints
      ? [{ href: '/channelpoints', icon: 'gem', label: t('nav.channelpoints'), active: section === 'channelpoints' }]
      : []),
    ...(canTimers ? [{ href: '/timers', icon: 'clock', label: t('nav.timers'), active: section === 'timers' }] : []),
    ...(canBilling
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

<!-- Hidden mark-read form the bell submits into; ?/markRead lives on the
     /settings route but SvelteKit actions can target any page. -->
<form method="POST" action="/settings?/markRead" use:enhance bind:this={markReadForm} hidden>
  <input type="hidden" name="id" value={markReadId ?? ''} />
</form>

<!-- Hidden peek form: submitted once when the bell dropdown first opens. -->
<form method="POST" action="/settings?/markPeeked" use:enhance bind:this={peekForm} hidden></form>

<!-- One toast host for the whole app; pages push via the shared toast() store. -->
<ToastHost />
