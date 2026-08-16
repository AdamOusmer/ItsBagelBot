<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary and unlicensed. See LICENSE.md.
  import '../app.css';
  import { RootShell } from '@bagel/shared';
  let { children } = $props();
</script>

<RootShell>{@render children()}</RootShell>
