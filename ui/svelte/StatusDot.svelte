<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import '../styles/elements/status-dot.css';
  import type { StatusDotTone } from '../lib/tone';

  let { tone, flat = false }: { tone: StatusDotTone; flat?: boolean } = $props();
</script>

<span class="bb-status-dot {tone}" class:flat aria-hidden="true"></span>
