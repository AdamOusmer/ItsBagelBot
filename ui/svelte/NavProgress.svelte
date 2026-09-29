<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  import '../styles/elements/nav-progress.css';

  let { active = false }: { active?: boolean } = $props();
</script>

<div class="bb-nav-progress" class:bb-nav-progress--active={active} role="presentation" aria-hidden="true"></div>
