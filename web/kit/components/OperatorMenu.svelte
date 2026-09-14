<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  // The signed-in operator: an avatar chip that opens a menu with the boards
  // shared with this account and a way out.
  //
  // Lifted out of Topbar.svelte when the strip became @bagel/ui's
  // `.bb-topbar`. It could not go with it, and the reasons are the plan's own
  // rule for what stays in kit: it reads `$app/navigation`, it renders a
  // `POST /auth/logout` form against a route only this app has, it draws an
  // avatar with a third-party engine (@luzir/bolota), and every string in it
  // comes from the console's i18n catalog. A design library that knew any one
  // of those could not be extracted.
  //
  // What it renders INTO is ui's `account` slot, so the strip does not know
  // this exists and this does not know how the strip is laid out.
  import '@bagel/ui/styles/elements/profile-menu.css';
  import { afterNavigate } from '$app/navigation';
  import Icon from '@bagel/ui/svelte/Icon.svelte';
  import Scroller from '@bagel/ui/svelte/Scroller.svelte';
  import Bolota from './Bolota.svelte';
  import type { DashboardLink } from '../lib/types';
  import { getI18n } from '../lib/i18n/context';

  // Falls back to English when no i18n context is set (admin), so the labels
  // are correct in every app without prop-drilling them through the shell.
  const { t } = getI18n();

  let {
    accountName,
    accountRole,
    dashboards = [],
    isDelegate = false,
    delegateExitHref = '',
    delegateExitLabel = ''
  }: {
    accountName: string;
    accountRole: string;
    // Boards shared with this user; renders a scrollable quick-switch list.
    // Empty (admin, or a user with no grants) hides the section.
    dashboards?: DashboardLink[];
    isDelegate?: boolean;
    delegateExitHref?: string;
    delegateExitLabel?: string;
  } = $props();

  let menuOpen = $state(false);

  // Drives the chip's Bolota: it wakes up on hover, independently of whether
  // the menu is open.
  let hovered = $state(false);

  // This lives in the persistent layout, so a shared-board link navigates
  // without unmounting it and would leave the menu hanging open. Close on any
  // completed navigation (which covers back/forward too).
  afterNavigate(() => (menuOpen = false));
</script>

<svelte:window onkeydown={(e) => { if (e.key === 'Escape') menuOpen = false; }} />

<button
  class="bb-profile-topbar__operator"
  class:bb-profile-topbar__open={menuOpen}
  title="{accountName} · {accountRole}"
  aria-label="{accountName} · {accountRole}"
  aria-expanded={menuOpen}
  aria-haspopup="menu"
  onclick={() => (menuOpen = !menuOpen)}
  onpointerenter={() => (hovered = true)}
  onpointerleave={() => (hovered = false)}
>
  <span class="bb-profile-topbar__avatar"><Bolota name={accountName} size={30} active={hovered || menuOpen} /></span>
  <span class="bb-profile-topbar__op-id">
    <b>{accountName}</b>
    <i>{accountRole}</i>
  </span>
</button>
{#if menuOpen}
  <!-- Click-away scrim; Escape via the window handler above. -->
  <!-- svelte-ignore a11y_no_static_element_interactions -->
  <div
    class="bb-profile__scrim"
    role="presentation"
    onclick={() => (menuOpen = false)}
    onkeydown={(e) => { if (e.key === 'Enter') menuOpen = false; }}
  ></div>
  <div class="bb-profile-topbar__op-menu" role="menu">
    <div class="bb-profile__head">
      <span class="bb-profile__portrait">
        <Bolota name={accountName} size={72} active={menuOpen} />
      </span>
      <b>{accountName}</b>
      <i>{accountRole}</i>
    </div>
    {#if dashboards.length}
      <!-- Boards shared with this user; the Scroller caps the list so a long
           roster never runs the menu off-screen. Each row jumps into that
           owner's dashboard via the /delegate/enter link. -->
      <div class="bb-profile-topbar__op-dash-group">
        <div class="bb-profile__section">{t('topbar.dashboards')}</div>
        <Scroller maxHeight="208px" role="group" aria-label={t('topbar.dashboards')}>
          <div class="bb-profile__list">
            {#each dashboards as d (d.href)}
              <a class="bb-profile__link" href={d.href} role="menuitem">
                <span class="bb-profile__avatar"><Bolota name={d.name} size={26} active={menuOpen} gate /></span>
                <span class="bb-profile__name">{d.name}</span>
              </a>
            {/each}
          </div>
        </Scroller>
      </div>
    {/if}
    {#if isDelegate}
      <div class="bb-profile-topbar__op-dash-group">
        <a class="bb-profile__link" href={delegateExitHref} role="menuitem">
          <span class="bb-profile__avatar"><Icon name="home" size={14} /></span>
          <span class="bb-profile__name">{delegateExitLabel}</span>
        </a>
      </div>
    {/if}
    <form method="POST" action="/auth/logout">
      <button type="submit" class="bb-profile-topbar__op-menu-item" role="menuitem">
        {t('topbar.logout')}
      </button>
    </form>
  </div>
{/if}
