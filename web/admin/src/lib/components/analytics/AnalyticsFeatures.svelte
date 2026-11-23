<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import Grid from '@bagel/ui/svelte/Grid.svelte';
  import Stack from '@bagel/ui/svelte/Stack.svelte';
  import AnalyticsSection from './AnalyticsSection.svelte';
  import AnalyticsTable from './AnalyticsTable.svelte';
  import AnalyticsRanking from './AnalyticsRanking.svelte';
  import type { AnalyticsView } from '#lib/analytics-page';

  let { features }: { features?: AnalyticsView['features']; } = $props();
</script>

<AnalyticsSection id="features">
  <AnalyticsTable id="modules" table={features?.modules} firstColumn="module" minWidth="960px" />
  <Grid cols={2} stackAt="md">
    <AnalyticsTable id="breadth" table={features?.breadth} firstColumn="modulesOn" facts={features?.breadthFacts} factsHeight="4.5rem" />
    <Stack gap={4}>
      <AnalyticsRanking id="integrations" bars={features?.integrations} />
      <AnalyticsRanking id="pagesHidden" bars={features?.pagesHidden} />
    </Stack>
  </Grid>
  <AnalyticsRanking id="dashboard" bars={features?.dashboard} />
</AnalyticsSection>
