<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import DonateCard from '@bagel/ui/svelte/DonateCard.svelte';
  import Bolota from '@bagel/ui/svelte/Bolota.svelte';
  import { getI18n } from '@bagel/kit/i18n/context';
  import { SITE } from '@bagel/kit/site-links';

  const { t } = getI18n();
  // The contact page's oven_fresh mascot, in its original green palette.
  const palette = { head: '#62a593', eye: '#111113' };
</script>

<DonateCard compact title={t('leaderboard.supportBotTitle')} cta={t('leaderboard.supportBotCta')} href={SITE.kofi}>
  <span style="--s: 104px"><Bolota name="oven_fresh" size={104} {palette} motionState="idle" active gate plate={false} /></span>
</DonateCard>
