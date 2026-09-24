<script lang="ts" module>
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import type { StageState } from '$lib/deploys/types';

  export type ChecklistItem = {
    id: string;
    label: string;
    meta: string;
    state: StageState;
    value: number | null | undefined;
    disabled?: boolean;
  };
</script>

<script lang="ts">
  import { prefersReducedMotion } from '@bagel/ui/lib/motion-query';
  import Icon from '@bagel/ui/svelte/Icon.svelte';

  let {
    items,
    focus,
    label,
    onselect
  }: {
    items: ChecklistItem[];
    focus: string;
    label: string;
    onselect: (id: string) => void;
  } = $props();

  const at = $derived(Math.max(items.findIndex((s) => s.id === focus), 0));

  let nav = $state<HTMLElement | null>(null);
  let placed = false;
  $effect(() => {
    void at;
    const row = nav?.querySelector<HTMLElement>('.row.focus');
    if (!nav || !row) return;
    const behavior = placed && !prefersReducedMotion() ? 'smooth' : 'instant';
    placed = true;
    if (nav.scrollWidth > nav.clientWidth) return nav.scrollTo({ left: row.offsetLeft - 12, behavior });
    const box = nav.parentElement;
    if (box && box.scrollHeight > box.clientHeight) {
      box.scrollTo({ top: nav.offsetTop + row.offsetTop - box.clientHeight / 2, behavior });
    }
  });
</script>

<nav class="checklist" aria-label={label} style="--at: {at}; --n: {items.length};" bind:this={nav}>
  <span class="glide" aria-hidden="true"></span>
  <ol>
    {#each items as s, i (s.id)}
      <li>
        <button
          type="button"
          class="row {s.state}"
          class:focus={s.id === focus}
          aria-current={s.id === focus ? 'step' : undefined}
          disabled={s.disabled}
          onclick={() => onselect(s.id)}
        >
          <span class="mark" aria-hidden="true">
            {#if s.state === 'succeeded'}
              <Icon name="check" size={12} />
            {:else if s.state === 'failed' || s.state === 'cancelled'}
              <Icon name="x" size={12} />
            {:else}
              <span class="num">{String(i + 1).padStart(2, '0')}</span>
            {/if}
          </span>
          <span class="text">
            <span class="label">{s.label}</span>
            <span class="meta">{s.meta}</span>
            <span class="track" aria-hidden="true">
              <span class="fill" class:indeterminate={s.value === null} style="--v: {s.value ?? 0};"></span>
            </span>
          </span>
        </button>
      </li>
    {/each}
  </ol>
</nav>

<style>
  .checklist {
    --row: clamp(44px, calc((100svh - 300px) / var(--n)), 60px);
    position: relative;
  }
  ol {
    position: relative;
    margin: 0;
    padding: 0;
    list-style: none;
  }
  .glide {
    position: absolute;
    left: 0;
    right: 0;
    top: 0;
    height: var(--row);
    border-radius: var(--bb-radius-sm);
    background: linear-gradient(100deg, rgba(var(--bb-tan-rgb), 0.18), rgba(var(--bb-green-glow-rgb), 0.08));
    box-shadow: inset 0 1px 0 rgba(255, 255, 255, 0.07);
    transform: translateY(calc(var(--at) * var(--row)));
    transition: transform 700ms var(--bb-ease-out-expo);
    pointer-events: none;
  }

  .row {
    position: relative;
    display: grid;
    grid-template-columns: 28px minmax(0, 1fr);
    align-items: center;
    gap: 12px;
    width: 100%;
    height: var(--row);
    padding: 0 12px;
    border: 0;
    background: none;
    text-align: left;
    color: var(--bb-muted);
    cursor: pointer;
    transition: transform var(--bb-dur-base) var(--bb-ease-out-expo);
  }
  .row:not(:disabled):hover {
    transform: translateX(3px);
  }
  .row:disabled {
    cursor: not-allowed;
    opacity: 0.4;
  }
  .row:focus-visible {
    outline: 2px solid var(--bb-tan);
    outline-offset: -2px;
    border-radius: var(--bb-radius-sm);
  }

  li:not(:last-child) .row::after {
    content: '';
    position: absolute;
    left: 25px;
    top: calc(50% + 14px);
    height: calc(var(--row) - 28px);
    width: 1px;
    background: var(--bb-border-strong);
  }
  li:not(:last-child) .row.succeeded::after,
  li:not(:last-child) .row.skipped::after {
    background: linear-gradient(180deg, var(--bb-tan), rgba(var(--bb-tan-rgb), 0.35));
  }

  .mark {
    display: grid;
    place-items: center;
    width: 28px;
    height: 28px;
    border-radius: 50%;
    border: 1px solid var(--bb-border-strong);
    background: var(--bb-card-bg);
    color: var(--bb-white);
    transition:
      background 400ms var(--bb-ease-out-expo),
      border-color 400ms var(--bb-ease-out-expo),
      color 400ms var(--bb-ease-out-expo);
  }
  .num {
    font-family: var(--bb-font-mono);
    font-size: 10.5px;
    letter-spacing: 0.04em;
  }
  .succeeded .mark {
    background: var(--bb-tan);
    border-color: var(--bb-tan);
    color: var(--bb-card-bg);
  }
  .running .mark,
  .waiting .mark {
    border-color: var(--bb-green-glow);
    color: var(--bb-green-glow);
    box-shadow:
      0 0 0 3px rgba(var(--bb-green-glow-rgb), 0.18),
      0 0 14px rgba(var(--bb-green-glow-rgb), 0.5);
  }
  .waiting .mark {
    border-style: dashed;
  }
  .failed .mark {
    background: var(--bb-status-error, #e5484d);
    border-color: var(--bb-status-error, #e5484d);
  }
  .skipped .mark,
  .cancelled .mark {
    opacity: 0.55;
  }

  .text {
    display: grid;
    gap: 4px;
    min-width: 0;
  }
  .label {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font: 600 14px/1.2 var(--bb-font-display);
    color: var(--bb-white);
  }
  .meta {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-family: var(--bb-font-mono);
    font-size: 11px;
    letter-spacing: 0.02em;
    color: rgba(255, 255, 255, 0.62);
    font-variant-numeric: tabular-nums;
  }
  .running .meta,
  .waiting .meta {
    color: var(--bb-green-glow);
  }
  .failed .meta {
    color: var(--bb-status-error, #e5484d);
  }

  .track {
    position: relative;
    height: 3px;
    overflow: hidden;
    border-radius: var(--bb-radius-pill);
    background: rgba(255, 255, 255, 0.08);
  }
  .fill {
    position: absolute;
    inset: 0;
    border-radius: inherit;
    transform-origin: left;
    transform: scaleX(var(--v));
    background: linear-gradient(90deg, var(--bb-tan), var(--bb-green-glow));
    transition: transform 700ms var(--bb-ease-out-expo);
  }
  .succeeded .fill,
  .skipped .fill {
    background: var(--bb-tan);
  }
  .failed .fill {
    background: var(--bb-status-error, #e5484d);
  }
  .fill.indeterminate {
    transform: translateX(-100%) scaleX(0.4);
    animation: scan 1.6s var(--bb-ease-out-expo) infinite;
  }
  @keyframes scan {
    from {
      transform: translateX(-100%) scaleX(0.4);
    }
    to {
      transform: translateX(250%) scaleX(0.4);
    }
  }


  @media (max-width: 960px) {
    .checklist {
      overflow-x: auto;
      scroll-snap-type: x mandatory;
      scrollbar-width: none;
    }
    ol {
      display: flex;
      gap: 4px;
    }
    li {
      flex: 0 0 auto;
      scroll-snap-align: start;
    }
    .glide,
    li .row::after {
      display: none;
    }
    .row {
      --row: 60px;
      grid-template-columns: 28px auto;
      width: auto;
      border-radius: var(--bb-radius-sm);
    }
    .row.focus {
      background: linear-gradient(100deg, rgba(var(--bb-tan-rgb), 0.18), rgba(var(--bb-green-glow-rgb), 0.08));
    }
    .row.focus .text {
      min-width: 140px;
    }
    .row:not(.focus) .text {
      display: none;
    }
  }

  @media (prefers-reduced-motion: reduce) {
    .glide,
    .row,
    .mark,
    .fill {
      transition: none;
    }
    .fill.indeterminate {
      animation: none;
      transform: scaleX(0.4);
    }
  }
</style>
