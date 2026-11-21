<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import Grid from '@bagel/ui/svelte/Grid.svelte';
  import AnalyticsSection from './AnalyticsSection.svelte';
  import AnalyticsTable from './AnalyticsTable.svelte';
  import AnalyticsSeries from './AnalyticsSeries.svelte';
  import AnalyticsRanking from './AnalyticsRanking.svelte';
  import type { AnalyticsView } from '#lib/analytics-page';

  let { reliability }: { reliability?: AnalyticsView['reliability']; } = $props();
</script>

<AnalyticsSection id="reliability">
  <Grid cols={2} stackAt="md">
    <AnalyticsTable id="drops" table={reliability?.drops} firstColumn="sendKind" />
    <AnalyticsSeries id="dailyDrops" series={reliability?.dailyDrops} />
    <AnalyticsRanking id="songs" bars={reliability?.songs} facts={reliability?.songFacts} />
    <AnalyticsRanking id="blocked" bars={reliability?.blocked} />
  </Grid>
  <AnalyticsTable id="providers" table={reliability?.providers} firstColumn="provider" />
  <AnalyticsTable id="moderation" table={reliability?.flags} firstColumn="rule" />
</AnalyticsSection>
