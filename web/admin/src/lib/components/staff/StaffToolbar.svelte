<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  import PageToolbar from '@bagel/ui/svelte/PageToolbar.svelte';
  import SearchInput from '@bagel/ui/svelte/SearchInput.svelte';
  import Button from '@bagel/ui/svelte/Button.svelte';
  import Stack from '@bagel/ui/svelte/Stack.svelte';
  import { getI18n } from '@bagel/kit/i18n/context';
  import FilterTabs from '#lib/components/FilterTabs.svelte';
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
    { value: 'all', label: 'admin.staff.filterAll' },
    { value: 'active', label: 'admin.staff.activeChip' },
    { value: 'inactive', label: 'admin.staff.inactiveChip' }
  ] as const;

  const filterOptions = $derived(FILTERS.map(({ value, label }) => ({
    value,
    label: t(label),
    count: counts ? numberFormat.format(counts[value]) : ''
  })));
</script>

<Stack gap={3}>
  <FilterTabs
    options={filterOptions}
    bind:value={() => access, (value) => (access = value as AccessFilter)}
    label={t('admin.staff.activeLabel')}
    disabled={!counts}
    countWidth="4ch"
  />
  <PageToolbar>
    {#snippet leading()}
      <SearchInput id="staff-search" bind:value={search} placeholder={t('admin.staff.search')} aria-label={t('admin.staff.search')} tall />
    {/snippet}
    {#snippet trailing()}
      <div class="add">
        <Button block disabled={busy} onclick={onAdd}>
          <bb-fit><span data-fit>{t('admin.staff.add')}</span></bb-fit>
        </Button>
      </div>
    {/snippet}
  </PageToolbar>
</Stack>

<style>
  .add {
    inline-size: 10rem;
    max-inline-size: 100%;
  }
</style>
