<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import Grid from '@bagel/ui/svelte/Grid.svelte';
  import AnalyticsSection from './AnalyticsSection.svelte';
  import AnalyticsSeries from './AnalyticsSeries.svelte';
  import AnalyticsFlow from './AnalyticsFlow.svelte';
  import AnalyticsRanking from './AnalyticsRanking.svelte';
  import AnalyticsTable from './AnalyticsTable.svelte';
  import AnalyticsFactsCard from './AnalyticsFactsCard.svelte';
  import type { AnalyticsView } from '#lib/analytics-page';

  let { growth }: { growth?: AnalyticsView['growth']; } = $props();
</script>

<AnalyticsSection id="growth">
  <AnalyticsSeries id="signups" series={growth?.signups} />
  <AnalyticsFlow id="gainLoss" gainLoss={growth?.gainLoss} />
  <Grid min="17rem">
    <AnalyticsFlow id="stops" flow={growth?.stops} />
    <AnalyticsFlow id="inactive" flow={growth?.inactive} />
    <AnalyticsFlow id="dormant" flow={growth?.dormant} />
  </Grid>
  <Grid min="16rem">
    <AnalyticsRanking id="funnel" bars={growth?.funnel} />
    <AnalyticsRanking id="churn" bars={growth?.churn} facts={growth?.churnFacts} />
    <AnalyticsRanking id="presets" bars={growth?.presets} />
  </Grid>
  <Grid cols={2} stackAt="md">
    <AnalyticsRanking id="activationAge" bars={growth?.activationAge} />
    <AnalyticsRanking id="onboarding" bars={growth?.onboarding} facts={growth?.onboarded} />
  </Grid>
  <AnalyticsTable id="imports" table={growth?.imports} firstColumn="importSource" />
  <AnalyticsTable id="signupSources" table={growth?.signupSources} firstColumn="via" />
  <Grid cols={2} stackAt="md">
    <AnalyticsRanking id="login" bars={growth?.login} />
    <AnalyticsFactsCard id="trials" items={growth?.trials} />
  </Grid>
</AnalyticsSection>
