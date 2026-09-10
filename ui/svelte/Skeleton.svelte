<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  //
  // Loading placeholder for streamed sections. Shape follows the content it
  // stands in for: text lines, a pill, or a block (card/tile).
  //
  // Size travels as custom properties (--skel-w / --skel-h), never as a
  // hand-built `style="width:…"` string. See ui/styles/elements/skeleton.css
  // for why: a CSP style-src without 'unsafe-inline' drops the attribute, and
  // the parity normaliser needs one serialisation to compare, not two.
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
