<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import '../styles/elements/status-dot.css';

  // Decoration beside text that names the status; never the only signal.
  let { tone }: { tone: 'success' | 'warning' | 'error' | 'neutral' } = $props();
</script>

<span class="bb-status-dot {tone}" aria-hidden="true"></span>
