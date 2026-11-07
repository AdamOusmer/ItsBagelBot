<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import Bolota from '@bagel/ui/svelte/Bolota.svelte';

  let { seed, active = false }: { seed: string; active?: boolean } = $props();
</script>

<span class="bb-chat__name bb-chat__name--bot">
  <span class="bb-chat__avatar" aria-hidden="true"><Bolota name={seed} size={20} {active} /></span>
  ItsBagelBot
</span>
