<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import Grid from '@bagel/ui/svelte/Grid.svelte';
  import AnalyticsSection from './AnalyticsSection.svelte';
  import AnalyticsRanking from './AnalyticsRanking.svelte';
  import AnalyticsTable from './AnalyticsTable.svelte';
  import type { AnalyticsView } from '#lib/analytics-page';

  let { community }: { community?: AnalyticsView['community']; } = $props();
</script>

<AnalyticsSection id="community">
  <Grid cols={2} stackAt="md">
    <AnalyticsRanking id="ticks" bars={community?.ticks} />
    <AnalyticsRanking id="timers" bars={community?.timers} />
  </Grid>
  <AnalyticsTable id="rounds" table={community?.rounds} firstColumn="game" minWidth="760px" />
  <AnalyticsTable id="entrants" table={community?.entrants} firstColumn="game" minWidth="620px" />
  <Grid cols={2} stackAt="md">
    <AnalyticsRanking id="points" bars={community?.points} />
    <AnalyticsTable id="rewards" table={community?.rewards} firstColumn="feature" minWidth="640px" />
  </Grid>
</AnalyticsSection>
