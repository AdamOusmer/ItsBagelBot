<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import Grid from '@bagel/ui/svelte/Grid.svelte';
  import AnalyticsSection from './AnalyticsSection.svelte';
  import AnalyticsTable from './AnalyticsTable.svelte';
  import AnalyticsRanking from './AnalyticsRanking.svelte';
  import type { AnalyticsView } from '#lib/analytics-page';
  import type { DonationsView } from '#lib/donations-view';
  import { getI18n } from '@bagel/kit/i18n/context';

  let { revenue, donations, donationsError }: { revenue?: AnalyticsView['revenue']; donations?: DonationsView; donationsError?: boolean; } = $props();
  const { t } = getI18n();
</script>

<AnalyticsSection id="revenue">
  <Grid cols={2} stackAt="md">
    <AnalyticsTable id="plans" table={revenue?.plans} firstColumn="plan" facts={revenue?.billingFacts} factsHeight="4.5rem" />
    <AnalyticsRanking id="sources" bars={revenue?.sources} />
    <AnalyticsRanking id="billing" bars={revenue?.billing} />
    <AnalyticsTable id="conversion" table={revenue?.conversion} firstColumn="signupWeek" />
  </Grid>
  <AnalyticsTable id="checkout" table={revenue?.checkout} firstColumn="plan" facts={revenue?.checkoutFacts} factsHeight="8.5rem" />
  <AnalyticsTable id="donations" table={donations?.totals} firstColumn="currency" minWidth="760px"
    facts={donations?.totalFacts} factsHeight="4.5rem" error={donationsError ? t('admin.analytics.donationsUnavailable') : undefined} />
  <Grid cols={2} stackAt="md">
    <AnalyticsTable id="donationMonths" table={donations?.months} firstColumn="month" />
    <AnalyticsRanking id="donationKinds" bars={donations?.kinds} />
  </Grid>
</AnalyticsSection>
