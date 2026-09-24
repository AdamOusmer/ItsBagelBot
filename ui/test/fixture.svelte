<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  let { text = 'hello' }: { text?: string } = $props();
</script>

<p class="bb-fixture" data-fixture>{text}</p>
