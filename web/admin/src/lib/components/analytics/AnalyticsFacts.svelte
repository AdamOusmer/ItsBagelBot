<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import Fact from '@bagel/ui/svelte/Fact.svelte';
  import FactList from '@bagel/ui/svelte/FactList.svelte';
  import { getI18n } from '@bagel/kit/i18n/context';
  import type { Fact as ViewFact } from '#lib/analytics-page';

  let { items }: { items: ViewFact[] } = $props();
  const { t } = getI18n();
</script>

<FactList layout="tiles">
  {#each items as fact (fact.key)}
    <Fact term={t(`admin.analytics.fact.${fact.key}`)} fit>{fact.value}</Fact>
  {/each}
</FactList>
