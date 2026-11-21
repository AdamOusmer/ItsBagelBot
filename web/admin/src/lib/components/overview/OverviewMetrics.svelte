<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import StatTile from '@bagel/ui/svelte/StatTile.svelte';
  import Skeleton from '@bagel/ui/svelte/Skeleton.svelte';
  import { getI18n } from '@bagel/kit/i18n/context';
  import type { UserStats } from '@bagel/kit';
  import type { ServiceHealth } from '#lib/server/services';
  import type { Panel } from '../../../routes/(admin)/+page.server';

  let { accounts, live, health }: {
    accounts: Promise<Panel<UserStats>>;
    live: Promise<number | null>;
    health: Promise<Panel<ServiceHealth[]>>;
  } = $props();

  const { t, locale } = getI18n();
  const format = new Intl.NumberFormat(locale);
  const unavailable = $derived(t('admin.overview.kpi.unavailable'));
  const liveLabel = $derived(t('admin.overview.kpi.live'));
</script>

{#snippet loading()}
  <div role="status" aria-label={t('common.loading')} aria-busy="true" class="placeholder">
    <Skeleton variant="block" height="100%" />
  </div>
{/snippet}

{#snippet tile(label: string, value: string | null, hint?: string)}
  <StatTile
    {label}
    value={value ?? '–'}
    delta={value === null ? unavailable : hint}
    fitCopy
    flat
    static
    style="height: 100%"
  />
{/snippet}

<div class="metrics">
  <div class="bb-stat-grid">
    <div class="tile">
      {#await accounts}
        {@render loading()}
      {:then { value, ok }}
        {@render tile(
          t('admin.overview.kpi.accounts'),
          ok ? format.format(value.total_users) : null,
          t('admin.overview.kpi.active', { count: format.format(value.active_users) })
        )}
      {/await}
    </div>

    <a class="tile live-tile" href="/users?live=1">
      {#await live}
        <span class="bb-sr-only">{liveLabel}</span>
        {@render loading()}
      {:then count}
        {@render tile(liveLabel, count === null ? null : format.format(count))}
      {/await}
    </a>

    <div class="tile">
      {#await accounts}
        {@render loading()}
      {:then { value, ok }}
        {@render tile(t('admin.overview.kpi.premium'), ok ? format.format(value.paid_users + value.vip_users) : null)}
      {/await}
    </div>

    <div class="tile">
      {#await health}
        {@render loading()}
      {:then { value, ok }}
        {@const healthy = value.filter((p) => p.ok).length}
        {@render tile(
          t('admin.overview.kpi.fleet'),
          ok ? `${format.format(healthy)} / ${format.format(value.length)}` : null,
          t('admin.overview.kpi.probes')
        )}
      {/await}
    </div>
  </div>
</div>

<style>
  .metrics {
    margin-block-end: var(--row-gap);
  }

  .placeholder {
    block-size: 100%;
  }

  .tile {
    block-size: 8.75rem;
    min-inline-size: 0;
  }

  @media (max-width: 760px) {
    .tile {
      block-size: 7.5rem;
    }
  }

  .live-tile {
    display: block;
    border-radius: var(--bb-radius-md);
    color: inherit;
    text-decoration: none;
  }

  .live-tile:hover {
    outline: 1px solid var(--bb-border-strong);
  }

  .live-tile:focus-visible {
    outline: 2px solid var(--bb-tan);
    outline-offset: 2px;
  }
</style>
