<script lang="ts">
  // Shared surface card, ported from the marketing site's Card.astro: solid
  // warm ink, tan hairline, 16px radius, and — when `hover` is on — the
  // cursor-tracked tan spotlight with a border brighten + small lift.
  import type { Snippet } from 'svelte';
  let {
    sheen = false,
    stat = false,
    hover = false,
    class: cls = '',
    children,
    ...rest
  }: { sheen?: boolean; stat?: boolean; hover?: boolean; class?: string; children: Snippet; [key: string]: unknown } = $props();

  let el = $state<HTMLDivElement | null>(null);

  function track(e: PointerEvent) {
    if (!hover || !el) return;
    const r = el.getBoundingClientRect();
    el.style.setProperty('--mx', `${(((e.clientX - r.left) / r.width) * 100).toFixed(2)}%`);
    el.style.setProperty('--my', `${(((e.clientY - r.top) / r.height) * 100).toFixed(2)}%`);
  }
</script>

<div
  class="card {sheen ? 'sheen' : ''} {stat ? 'stat' : ''} {hover ? 'hoverable' : ''} {cls}"
  bind:this={el}
  onpointermove={hover ? track : undefined}
  {...rest}
>
  {#if hover}<span class="spotlight" aria-hidden="true"></span>{/if}
  {@render children()}
</div>

<style>
  .card {
    background: var(--bb-card-bg);
    border: 1px solid var(--bb-border);
    border-radius: 8px;
    padding: var(--card-pad);
    position: relative;
    overflow: hidden;
    transition:
      border-color 360ms var(--bb-ease-out-expo),
      transform 360ms var(--bb-ease-out-expo);
  }
  .card.sheen::before { content: ""; position: absolute; inset: 0; pointer-events: none; border-radius: inherit;
    background: radial-gradient(circle at 88% 0%, var(--glow-green, rgba(82,183,136,0.16)), transparent 50%); }
  .card.stat { padding: calc(20px * var(--d)); }

  .spotlight {
    position: absolute;
    inset: 0;
    pointer-events: none;
    opacity: 0;
    background: radial-gradient(
      420px circle at var(--mx, 50%) var(--my, 50%),
      var(--glow-tan, rgba(201, 168, 124, 0.18)),
      transparent 60%
    );
    transition: opacity 360ms var(--bb-ease-out-expo);
  }

  @media (hover: hover) and (pointer: fine) {
    .card.hoverable:hover {
      border-color: rgba(201, 168, 124, 0.38);
      transform: translateY(-3px);
    }
    .card.hoverable:hover .spotlight { opacity: 1; }
  }
</style>
