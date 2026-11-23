<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  let { text }: { text?: string | null } = $props();

  let last = $state('');

  $effect(() => {
    if (text) last = text;
  });
</script>

{text || last}
