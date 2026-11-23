<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import Grid from '@bagel/ui/svelte/Grid.svelte';
  import AnalyticsSection from './AnalyticsSection.svelte';
  import AnalyticsRanking from './AnalyticsRanking.svelte';
  import AnalyticsFactsCard from './AnalyticsFactsCard.svelte';
  import type { AnalyticsView } from '#lib/analytics-page';

  let { audience }: { audience?: AnalyticsView['audience']; } = $props();
</script>

<AnalyticsSection id="audience">
  <AnalyticsRanking id="sizes" bars={audience?.sizes} />
  <AnalyticsRanking id="locales" bars={audience?.locales} />
  <AnalyticsFactsCard id="delegating" items={audience?.delegating} />
  <Grid cols={2} stackAt="md">
    <AnalyticsRanking id="runtime" bars={audience?.runtime} />
    <AnalyticsRanking id="desktop" bars={audience?.desktop} />
  </Grid>
</AnalyticsSection>
