<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import '../styles/elements/status-dot.css';

  let { tone, flat = false }: { tone: 'success' | 'warning' | 'error' | 'neutral'; flat?: boolean } = $props();
</script>

<span class="bb-status-dot {tone}" class:flat aria-hidden="true"></span>
