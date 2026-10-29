<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import Bolota from '@bagel/kit/components/Bolota.svelte';

  type Sequence = 'entrance' | 'burst' | 'orbit' | 'comet';

  let {
    expression,
    sequence,
    sequenceKey,
    size = 200
  }: {
    expression: string;
    sequence: Sequence;
    sequenceKey: number;
    size?: number;
  } = $props();
</script>

<div class="mascot" aria-hidden="true">
  <Bolota
    name="ItsBagelBot"
    {size}
    active
    follow
    cycle={false}
    {expression}
    {sequence}
    {sequenceKey}
    sequenceFor={1100}
    sequenceHold={240}
  />
</div>

<style>
  .mascot {
    display: flex;
    justify-content: center;
    padding: 18px 0 8px;
  }
</style>
