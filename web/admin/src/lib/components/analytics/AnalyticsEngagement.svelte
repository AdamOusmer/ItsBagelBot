<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import Grid from '@bagel/ui/svelte/Grid.svelte';
  import AnalyticsSection from './AnalyticsSection.svelte';
  import AnalyticsSeries from './AnalyticsSeries.svelte';
  import AnalyticsFactsCard from './AnalyticsFactsCard.svelte';
  import type { AnalyticsView } from '#lib/analytics-page';

  let { engagement }: { engagement?: AnalyticsView['engagement']; } = $props();
</script>

<AnalyticsSection id="engagement">
  <Grid cols={2} stackAt="md">
    <AnalyticsSeries id="active" series={engagement?.active} />
    <AnalyticsSeries id="live" series={engagement?.live} />
    <AnalyticsSeries id="messages" series={engagement?.messages} />
    <AnalyticsSeries id="answered" series={engagement?.answered} />
  </Grid>
  <AnalyticsFactsCard id="activity" items={engagement?.facts} height="12rem" />
</AnalyticsSection>
