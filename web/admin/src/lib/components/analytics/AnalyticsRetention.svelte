<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import Table from '@bagel/ui/svelte/Table.svelte';
  import AnalyticsSection from './AnalyticsSection.svelte';
  import AnalyticsPanel from './AnalyticsPanel.svelte';
  import { getI18n } from '@bagel/kit/i18n/context';
  import type { AnalyticsView } from '#lib/analytics-page';

  let { retention }: { retention?: AnalyticsView['retention'] } = $props();
  const { t } = getI18n();
  const weekColumns = $derived(Array.from({ length: retention?.horizon ?? 0 }, (_, week) => week));
</script>

<AnalyticsSection id="retention">
  <AnalyticsPanel id="cohorts" height="20rem" loading={!retention} empty={retention?.rows.length === 0}>
    {#if retention}
      <div class="cohort-viewport">
        <Table label={t('admin.analytics.card.cohorts')} compact minWidth="880px">
          <thead>
            <tr>
              <th scope="col">{t('admin.analytics.column.signupWeek')}</th>
              <th scope="col" class="r">{t('admin.analytics.column.signups')}</th>
              {#each weekColumns as week (week)}
                <th scope="col" class="r">{t('admin.analytics.column.weekN', { n: String(week) })}</th>
              {/each}
            </tr>
          </thead>
          <tbody>
            {#each retention.rows as line (line.week)}
              <tr>
                <th scope="row">{line.week}</th>
                <td class="r">{line.size}</td>
                {#each line.cells as cell}
                  {#if cell}
                    <td class="heat r" style="--heat: {cell.heat}">{cell.percent}<small>{cell.count}</small></td>
                  {:else}
                    <td class="heat"></td>
                  {/if}
                {/each}
              </tr>
            {/each}
          </tbody>
        </Table>
      </div>
    {/if}
  </AnalyticsPanel>
</AnalyticsSection>

<style>
  .cohort-viewport {
    display: grid;
    grid-template-columns: minmax(0, 1fr);
    grid-template-rows: minmax(0, 1fr);
    block-size: 20rem;
    min-inline-size: 0;
  }
</style>
