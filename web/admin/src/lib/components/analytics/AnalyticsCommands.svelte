<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import Grid from '@bagel/ui/svelte/Grid.svelte';
  import AnalyticsSection from './AnalyticsSection.svelte';
  import AnalyticsTable from './AnalyticsTable.svelte';
  import AnalyticsFactsCard from './AnalyticsFactsCard.svelte';
  import AnalyticsRanking from './AnalyticsRanking.svelte';
  import type { AnalyticsView } from '#lib/analytics-page';

  let { commands }: { commands?: AnalyticsView['commands']; } = $props();
</script>

<AnalyticsSection id="commands">
  <AnalyticsTable id="builtIn" table={commands?.builtIn} firstColumn="command" />
  <Grid cols={3} stackAt="md">
    <AnalyticsFactsCard id="custom" items={commands?.custom} />
    <AnalyticsRanking id="perms" bars={commands?.perms} />
    <AnalyticsRanking id="flags" bars={commands?.flags} />
  </Grid>
</AnalyticsSection>
