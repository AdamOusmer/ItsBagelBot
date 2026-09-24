<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import type { Snippet } from 'svelte';
  import { flight } from './flight';

  let {
    dir,
    title,
    body,
    headingId,
    heading = $bindable(null),
    center = false,
    kicker,
    note,
    children,
    actions
  }: {
    dir: number;
    title: string;
    body: string;
    headingId: string;
    heading?: HTMLHeadingElement | null;
    center?: boolean;
    kicker: Snippet;
    note?: Snippet;
    children?: Snippet;
    actions?: Snippet;
  } = $props();

  const { arrive, depart } = flight(() => dir);
</script>

<article class="scene" class:center>
  <p class="kicker" in:arrive={{ i: 0 }} out:depart={{ i: 0 }}>{@render kicker()}</p>
  <h1 id={headingId} class="title" tabindex="-1" bind:this={heading} in:arrive={{ i: 1 }} out:depart={{ i: 1 }}>
    {title}
  </h1>
  <p class="body" in:arrive={{ i: 2 }} out:depart={{ i: 2 }}>{body}</p>
  <div class="note" in:arrive={{ i: 3 }} out:depart={{ i: 3 }}>{@render note?.()}</div>
  <div class="control" in:arrive={{ i: 3 }} out:depart={{ i: 3 }}>{@render children?.()}</div>
  <div class="actions" in:arrive={{ i: 4 }} out:depart={{ i: 4 }}>{@render actions?.()}</div>
</article>

<style>
  .scene {
    grid-area: 1 / 1;
    display: flex;
    flex-direction: column;
    min-width: 0;
  }
  .scene.center {
    align-items: center;
    text-align: center;
  }
  .kicker {
    display: flex;
    flex-wrap: wrap;
    align-items: baseline;
    gap: 8px;
    margin: 0 0 14px;
    font-family: var(--bb-font-mono);
    font-size: 11.5px;
    letter-spacing: 0.14em;
    text-transform: uppercase;
    color: var(--bb-tan);
  }
  .title {
    margin: 0 0 16px;
    font-family: var(--bb-font-display);
    font-weight: 700;
    font-size: clamp(1.9rem, min(4.2vw, 6.4svh), 3.6rem);
    line-height: 1.04;
    letter-spacing: -0.03em;
    color: var(--bb-white);
    background: linear-gradient(100deg, var(--bb-white) 0 40%, var(--bb-tan-pale) 50%, var(--bb-white) 60% 100%);
    background-size: 260% 100%;
    background-position: 120% 0;
    -webkit-background-clip: text;
    background-clip: text;
    -webkit-text-fill-color: transparent;
    animation: sheen 1.8s var(--bb-ease-out-expo) 520ms both;
    outline: none;
  }
  .body {
    margin: 0;
    max-width: 56ch;
    font-family: var(--bb-font-body);
    font-size: clamp(1rem, 1.3vw, 1.1rem);
    line-height: 1.6;
    color: rgba(255, 255, 255, 0.72);
  }
  .note {
    width: 100%;
    max-width: 56ch;
    margin-top: 16px;
  }
  .control {
    width: 100%;
    margin-top: 22px;
    text-align: left;
  }
  .actions {
    position: sticky;
    bottom: 0;
    z-index: 2;
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 10px 14px;
    min-height: 44px;
    margin-top: 22px;
    padding: 10px 0 4px;
    background: linear-gradient(180deg, transparent, rgba(var(--bb-black-rgb), 0.78) 40%);
    backdrop-filter: blur(6px);
  }
  .scene.center .actions {
    justify-content: center;
  }
  .note:empty,
  .control:empty,
  .actions:empty {
    display: none;
  }

  @keyframes sheen {
    from {
      background-position: 120% 0;
    }
    to {
      background-position: -40% 0;
    }
  }

  @media (prefers-reduced-motion: reduce) {
    .title {
      animation: none;
      background: none;
      -webkit-text-fill-color: currentColor;
    }
  }
</style>
