<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import Table from '@bagel/ui/svelte/Table.svelte';
  import Tag from '@bagel/ui/svelte/Tag.svelte';
  import Skeleton from '@bagel/ui/svelte/Skeleton.svelte';
  import AnalyticsFacts from './AnalyticsFacts.svelte';
  import AnalyticsPanel from './AnalyticsPanel.svelte';
  import { getI18n } from '@bagel/kit/i18n/context';
  import type { ComponentProps } from 'svelte';
  import type { DataTable, Fact, Name } from '#lib/analytics-page';

  let { id, firstColumn, table, minWidth, facts, factsHeight, error }: {
    id: string; firstColumn: string; table?: DataTable; minWidth?: string; facts?: Fact[];
    factsHeight?: string; error?: string;
  } = $props();
  const { t } = getI18n();
  const title = $derived(t(`admin.analytics.card.${id}`));
  const nameText = (name: Name) => 'text' in name ? name.text : t(name.key, name.params);
  type TagTone = ComponentProps<typeof Tag>['tone'];
</script>

<AnalyticsPanel {id} height="12rem" loading={!table} empty={table?.rows.length === 0} {error} footerHeight={factsHeight}>
  {#snippet footer()}
    {#if !table}
      <div aria-hidden="true"><Skeleton variant="block" height={factsHeight} /></div>
    {:else if table.rows.length && facts}
      <AnalyticsFacts items={facts} />
    {/if}
  {/snippet}
  {#if table}
    <div class="table-viewport">
      <Table label={title} {minWidth}>
        <thead>
          <tr>
            <th scope="col">{t(`admin.analytics.column.${firstColumn}`)}</th>
            {#each table.columns as column (column)}
              <th scope="col" class="r">{t(`admin.analytics.column.${column}`)}</th>
            {/each}
          </tr>
        </thead>
        <tbody>
          {#each table.rows as row (row.id)}
            <tr>
              <th scope="row">
                {#if id === 'plans'}
                  <Tag tone={row.id as TagTone}>{nameText(row.name)}</Tag>
                {:else}
                  {nameText(row.name)}
                {/if}
              </th>
              {#each row.cells as cell}<td class="r">{cell.text}</td>{/each}
            </tr>
          {/each}
        </tbody>
      </Table>
    </div>
  {/if}
</AnalyticsPanel>

<style>
  .table-viewport {
    display: grid;
    grid-template-columns: minmax(0, 1fr);
    grid-template-rows: minmax(0, 1fr);
    block-size: 12rem;
    min-inline-size: 0;
  }
</style>
