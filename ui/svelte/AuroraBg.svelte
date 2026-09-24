<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  import '../styles/orbs.css';
  import '../styles/elements/aurora.css';

  let { class: className = '', ...rest }: { class?: string; [key: string]: unknown } = $props();

  const classes = $derived(['bb-aurora', className || null].filter(Boolean).join(' '));
</script>

<div class={classes} aria-hidden="true" {...rest}><span
    class="bb-orb bb-orb--aurora-green bb-orb--drift-1 bb-aurora__o1"
  ></span><span class="bb-orb bb-orb--aurora-tan bb-orb--drift-2 bb-aurora__o2"></span><span
    class="bb-orb bb-orb--aurora-green-soft bb-orb--drift-3 bb-aurora__o3"
  ></span></div>
