<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import Grid from '@bagel/ui/svelte/Grid.svelte';
  import Stack from '@bagel/ui/svelte/Stack.svelte';
  import AnalyticsSection from './AnalyticsSection.svelte';
  import AnalyticsStat from './AnalyticsStat.svelte';
  import AnalyticsRanking from './AnalyticsRanking.svelte';
  import AnalyticsTable from './AnalyticsTable.svelte';
  import type { AnalyticsView } from '#lib/analytics-page';
  import { getI18n } from '@bagel/kit/i18n/context';

  let { recipes }: { recipes?: AnalyticsView['recipes']; } = $props();
  const { t } = getI18n();
</script>

<AnalyticsSection id="recipes">
  <Grid cols={2} stackAt="md">
    <Stack gap={4}>
      <AnalyticsStat metric={recipes?.channels} label={t('admin.analytics.fact.recipeChannelsWeek')} />
      <AnalyticsRanking id="recipeRuns" bars={recipes?.runs} />
    </Stack>
    <AnalyticsTable id="recipeOutcomes" table={recipes?.outcomes} firstColumn="trigger" />
  </Grid>
  <AnalyticsTable id="recipeTotals" table={recipes?.totals} firstColumn="trigger" />
</AnalyticsSection>
