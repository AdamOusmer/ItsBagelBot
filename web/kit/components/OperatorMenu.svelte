<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import '@bagel/ui/styles/elements/profile-menu.css';
  import { afterNavigate } from '$app/navigation';
  import Icon from '@bagel/ui/svelte/Icon.svelte';
  import Scroller from '@bagel/ui/svelte/Scroller.svelte';
  import Bolota from './Bolota.svelte';
  import type { DashboardLink } from '../lib/types';
  import { getI18n } from '../lib/i18n/context';

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
    dashboards?: DashboardLink[];
    isDelegate?: boolean;
    delegateExitHref?: string;
    delegateExitLabel?: string;
  } = $props();

  let menuOpen = $state(false);

  let hovered = $state(false);

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
