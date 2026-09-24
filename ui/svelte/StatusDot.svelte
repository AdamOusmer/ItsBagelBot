<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import '../styles/elements/status-dot.css';

  let { tone }: { tone: 'success' | 'warning' | 'error' | 'neutral' } = $props();
</script>

<span class="bb-status-dot {tone}" aria-hidden="true"></span>
