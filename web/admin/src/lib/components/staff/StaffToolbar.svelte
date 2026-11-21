<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  import PageToolbar from '@bagel/ui/svelte/PageToolbar.svelte';
  import SearchInput from '@bagel/ui/svelte/SearchInput.svelte';
  import Chip from '@bagel/ui/svelte/Chip.svelte';
  import Button from '@bagel/ui/svelte/Button.svelte';
  import Skeleton from '@bagel/ui/svelte/Skeleton.svelte';
  import { getI18n } from '@bagel/kit/i18n/context';
  import type { AccessFilter } from './staff-roles';

  let {
    search = $bindable(''),
    access = $bindable('all'),
    counts,
    busy,
    onAdd
  }: {
    search?: string;
    access?: AccessFilter;
    counts?: Record<AccessFilter, number>;
    busy: boolean;
    onAdd: () => void;
  } = $props();

  const { t, locale } = getI18n();
  const numberFormat = new Intl.NumberFormat(locale);

  const FILTERS = [
    { value: 'all', label: 'admin.staff.filterAll', tone: undefined },
    { value: 'active', label: 'admin.staff.activeChip', tone: 'free' },
    { value: 'inactive', label: 'admin.staff.inactiveChip', tone: 'inactive' }
  ] as const;
</script>

<div class="toolbar">
  <PageToolbar class="staff-toolbar">
    {#snippet leading()}
      <div class="directory-controls">
        <SearchInput id="staff-search" bind:value={search} placeholder={t('admin.staff.search')} aria-label={t('admin.staff.search')} fill />
        <div class="access-filters" role="group" aria-label={t('admin.staff.activeLabel')}>
          {#each FILTERS as { value, label, tone } (value)}
            <Chip {tone} pressed={access === value} disabled={!counts} onclick={() => (access = value)}
              style="min-width: 0; justify-content: space-between">
              <span class="filter-name"><bb-fit><span data-fit>{t(label)}</span></bb-fit></span>
              <span class="count">
                {#if counts}
                  <bb-fit><span data-fit>{numberFormat.format(counts[value])}</span></bb-fit>
                {:else}
                  <Skeleton width="1.5ch" />
                {/if}
              </span>
            </Chip>
          {/each}
        </div>
      </div>
    {/snippet}
    {#snippet trailing()}
      <div class="add">
        <Button block disabled={busy} onclick={onAdd}>
          <bb-fit><span data-fit>{t('admin.staff.add')}</span></bb-fit>
        </Button>
      </div>
    {/snippet}
  </PageToolbar>
</div>

<style>
  .toolbar :global(.staff-toolbar) {
    flex-wrap: wrap;
  }

  .directory-controls {
    display: flex;
    flex: 1 1 33rem;
    gap: var(--bb-space-3);
    min-inline-size: 0;
    align-items: center;
  }

  .access-filters {
    display: grid;
    grid-template-columns: repeat(3, minmax(0, 1fr));
    gap: var(--bb-space-1);
    inline-size: min(100%, 21rem);
    flex: none;
  }

  .filter-name {
    flex: 1;
    min-inline-size: 0;
    text-align: start;
  }

  .count {
    inline-size: 4ch;
    flex: none;
    text-align: end;
    font-variant-numeric: tabular-nums;
  }

  .add {
    inline-size: 10rem;
    max-inline-size: 100%;
  }

  @media (max-width: 720px) {
    .directory-controls {
      display: grid;
      flex-basis: 100%;
    }

    .access-filters {
      inline-size: 100%;
    }
  }
</style>
