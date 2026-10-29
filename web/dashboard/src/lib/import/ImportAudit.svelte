<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import Text from '@bagel/ui/svelte/Text.svelte';

  let { text }: { text: string } = $props();
</script>

<div class="audit"><Text size="xs" mono tone="muted">{text}</Text></div>

<style>
  .audit { margin-top: 24px; }
</style>
