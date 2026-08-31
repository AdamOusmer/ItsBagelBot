<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  // Quiet rail entry: icon + sentence-case label. Deliberately NOT the ledger
  // row (NavItem): no index column, no mono uppercase. The active state is
  // carried by the rail's single gliding highlight, so this row only changes
  // colour — two competing active treatments read as a rendering bug.
  import Icon from './Icon.svelte';
  import type { IconName } from '../lib/icons';
  import { getI18n } from '../lib/i18n/context';

  const { t } = getI18n();

  let {
    href,
    icon,
    label,
    active = false,
    locked = false,
    count
  }: {
    href: string;
    icon: IconName;
    label: string;
    active?: boolean;
    locked?: boolean;
    count?: string | number;
  } = $props();
</script>

{#if locked}
  <span class="rail-item locked">
    <Icon name={icon} /> <span class="lbl">{label}</span>
    <Icon name="lock" size={13} />
    <span class="sr-only">{t('nav.lockedBroadcaster')}</span>
  </span>
{:else}
  <a class="rail-item {active ? 'active' : ''}" {href} aria-current={active ? 'page' : undefined}>
    <Icon name={icon} /> <span class="lbl">{label}</span>
    {#if count !== undefined}<span class="count">{count}</span>{/if}
  </a>
{/if}

<style>
  .rail-item {
    position: relative; z-index: 1;
    display: flex; align-items: center; gap: 12px;
    height: 40px; box-sizing: border-box; padding: 0 12px;
    border-radius: 10px; text-decoration: none;
    font-family: var(--bb-font-sans); font-weight: 500; font-size: 13.5px;
    color: var(--bb-muted);
    transition: color var(--bb-dur-fast) var(--bb-ease-out-expo);
  }
  .rail-item :global(svg) {
    width: 17px; height: 17px; stroke: currentColor; fill: none;
    stroke-width: 1.6; stroke-linecap: round; stroke-linejoin: round; flex-shrink: 0;
  }
  .lbl { flex: 1; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
  .count { font-family: var(--bb-font-mono); font-size: 11px; color: var(--bb-tan); }
  .rail-item:hover { color: var(--bb-white); }
  .rail-item.active { color: var(--bb-tan-pale); }
  .rail-item.locked { opacity: 0.45; cursor: not-allowed; }
</style>
