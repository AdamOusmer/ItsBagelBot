<script lang="ts">
  // Loading placeholder for streamed sections. Shape follows the content it
  // stands in for: text lines, a pill, or a block (card/tile).
  let {
    variant = 'text' as 'text' | 'pill' | 'block',
    width = undefined as string | undefined,
    height = undefined as string | undefined,
    lines = 1
  } = $props();
</script>

{#if variant === 'text' && lines > 1}
  <span class="skel-lines" style="width: {width ?? '100%'}">
    {#each Array(lines) as _, i (i)}
      <span class="skel text" style="width: {i === lines - 1 ? '60%' : '100%'}"></span>
    {/each}
  </span>
{:else}
  <span
    class="skel {variant}"
    style="{width ? `width:${width};` : ''}{height ? `height:${height};` : ''}"
  ></span>
{/if}

<style>
  .skel {
    display: inline-block;
    background: linear-gradient(
      100deg,
      rgba(255, 255, 255, 0.04) 40%,
      rgba(255, 255, 255, 0.09) 50%,
      rgba(255, 255, 255, 0.04) 60%
    );
    background-size: 200% 100%;
    animation: shimmer 1.4s ease-in-out infinite;
    border-radius: var(--bb-radius-sm, 8px);
  }
  .text { height: 0.9em; width: 8ch; vertical-align: middle; }
  .pill { height: 22px; width: 72px; border-radius: 999px; }
  .block { display: block; width: 100%; height: 64px; border-radius: var(--bb-radius-md, 10px); }

  .skel-lines { display: flex; flex-direction: column; gap: 8px; }

  @keyframes shimmer {
    0% { background-position: 200% 0; }
    100% { background-position: -200% 0; }
  }

  @media (prefers-reduced-motion: reduce) {
    .skel { animation: none; }
  }
</style>
