<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  // The rail's account foot. When the rail is on screen this is the account
  // surface: the row opens the switcher menu (shared dashboards + delegate
  // exit) that used to live behind the topbar avatar, so the signed-in user
  // has one place to be, not two.
  import '@bagel/ui/styles/elements/profile-menu.css';
  import { afterNavigate } from '$app/navigation';
  import Button from '@bagel/ui/svelte/Button.svelte';
  import Bolota from './Bolota.svelte';
  import Icon from '@bagel/ui/svelte/Icon.svelte';
  import Scroller from '@bagel/ui/svelte/Scroller.svelte';
  import type { DashboardLink } from '../lib/types';
  import { getI18n } from '../lib/i18n/context';

  // Falls back to English when no i18n context is set (admin).
  const { t } = getI18n();

  let {
    name,
    role,
    dashboards = [],
    isDelegate = false,
    delegateExitHref = '',
    delegateExitLabel = ''
  }: {
    name: string;
    role: string;
    // Boards shared with this user; renders a scrollable quick-switch list in
    // the account menu. Empty (e.g. a user with no grants) hides it.
    dashboards?: DashboardLink[];
    isDelegate?: boolean;
    delegateExitHref?: string;
    delegateExitLabel?: string;
  } = $props();

  // Wakes the Bolota engine while the pointer is over the account row.
  let hovered = $state(false);

  // Nothing to switch to and nowhere to exit back to: the row stays a plain
  // readout instead of a button that opens an empty menu.
  const hasMenu = $derived(dashboards.length > 0 || isDelegate);
  let menuOpen = $state(false);

  // The rail lives in the persistent layout, so a shared-dashboard link in the
  // menu navigates without unmounting it, leaving the menu open. Close it on
  // any completed navigation (covers back/forward too).
  afterNavigate(() => (menuOpen = false));
</script>

<svelte:window onkeydown={(e) => { if (e.key === 'Escape') menuOpen = false; }} />

<div class="bb-profile-rail__side-foot">
  {#if hasMenu}
    <button
      class="bb-profile-rail__account bb-profile-rail__account--btn"
      class:bb-profile-rail__open={menuOpen}
      type="button"
      aria-expanded={menuOpen}
      aria-haspopup="menu"
      onclick={() => (menuOpen = !menuOpen)}
      onpointerenter={() => (hovered = true)}
      onpointerleave={() => (hovered = false)}
    >
      <span class="bb-profile-rail__avatar"><Bolota name={name} size={34} active={hovered || menuOpen} /></span>
      <span class="bb-profile-rail__who">
        <b>{name}</b>
        <span>{role}</span>
      </span>
      <span class="bb-profile-rail__chev" class:bb-profile-rail__open={menuOpen} aria-hidden="true">
        <svg viewBox="0 0 24 24" width="14" height="14"><polyline points="9 6 15 12 9 18"></polyline></svg>
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
      <div class="bb-profile-rail__foot-menu" role="menu">
        <!-- Centrepiece, carried over from the topbar menu this replaced: a big
             Bolota on its own plate, name and role stacked under it. -->
        <div class="bb-profile__head">
          <span class="bb-profile__portrait">
            <Bolota name={name} size={72} active={menuOpen} />
          </span>
          <b>{name}</b>
          <i>{role}</i>
        </div>
        {#if dashboards.length}
          <!-- The Scroller caps the list so a long roster never runs the menu
               off the top of the rail. Each row jumps into that owner's
               dashboard via the /delegate/enter link. -->
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
        {/if}
        {#if isDelegate}
          <a class="bb-profile__link" href={delegateExitHref} role="menuitem">
            <span class="bb-profile__avatar"><Icon name="home" size={14} /></span>
            <span class="bb-profile__name">{delegateExitLabel}</span>
          </a>
        {/if}
      </div>
    {/if}
  {:else}
    <div class="bb-profile-rail__account" role="group" onpointerenter={() => (hovered = true)} onpointerleave={() => (hovered = false)}>
      <div class="bb-profile-rail__avatar"><Bolota name={name} size={34} active={hovered} /></div>
      <div class="bb-profile-rail__who">
        <b>{name}</b>
        <span>{role}</span>
      </div>
    </div>
  {/if}
  <form method="POST" action="/auth/logout" onsubmit={() => localStorage.removeItem('bb-onboarded')}>
    <Button variant="ghost" type="submit" style="width:100%;justify-content:center;margin-top:10px">
      {t('topbar.logout')}
    </Button>
  </form>
</div>
