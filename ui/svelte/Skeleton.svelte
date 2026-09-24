<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  let {
    variant = 'text' as 'text' | 'pill' | 'block',
    width = undefined as string | undefined,
    height = undefined as string | undefined,
    lines = 1
  } = $props();

  const size = (w?: string, h?: string) =>
    `${w ? `--skel-w:${w};` : ''}${h ? `--skel-h:${h};` : ''}` || undefined;
</script>

{#if variant === 'text' && lines > 1}
  <span class="bb-skel-lines" style={size(width ?? '100%')}>
    {#each Array(lines) as _, i (i)}
      <span class="bb-skel bb-skel--text" style={size(i === lines - 1 ? '60%' : '100%')}></span>
    {/each}
  </span>
{:else}
  <span class="bb-skel bb-skel--{variant}" style={size(width, height)}></span>
{/if}
