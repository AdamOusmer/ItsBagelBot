<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import type { Snippet } from 'svelte';
  import Eyebrow from '@bagel/ui/svelte/Eyebrow.svelte';
  import Heading from '@bagel/ui/svelte/Heading.svelte';
  import Lead from '@bagel/ui/svelte/Lead.svelte';
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

  const setHeading = (element: HTMLElement | null) => {
    heading = element instanceof HTMLHeadingElement ? element : null;
  };
</script>

<article class="scene" class:center>
  <div class="kicker" in:arrive={{ i: 0 }} out:depart={{ i: 0 }}><Eyebrow as="p">{@render kicker()}</Eyebrow></div>
  <div class="title" in:arrive={{ i: 1 }} out:depart={{ i: 1 }}>
    <Heading level={2} as="h1" id={headingId} tabindex={-1} bind:element={() => heading, setHeading}><span class="sheen">{title}</span></Heading>
  </div>
  <div in:arrive={{ i: 2 }} out:depart={{ i: 2 }}><Lead>{body}</Lead></div>
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
    margin-bottom: var(--bb-space-3);
  }
  .title {
    margin-bottom: var(--bb-space-4);
  }
  .sheen {
    background: linear-gradient(100deg, var(--bb-white) 0 40%, var(--bb-tan-pale) 50%, var(--bb-white) 60% 100%);
    background-size: 260% 100%;
    background-position: 120% 0;
    -webkit-background-clip: text;
    background-clip: text;
    -webkit-text-fill-color: transparent;
    animation: sheen calc(var(--bb-dur-slow) * 3) var(--bb-ease-out-expo) var(--bb-dur-slow) both;
  }
  .note {
    width: 100%;
    max-width: 56ch;
    margin-top: var(--bb-space-4);
  }
  .control {
    width: 100%;
    margin-top: var(--bb-space-5);
    text-align: left;
  }
  .actions {
    position: sticky;
    bottom: 0;
    z-index: 2;
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--bb-space-2) var(--bb-space-3);
    min-height: 44px;
    margin-top: var(--bb-space-5);
    padding: var(--bb-space-2) 0 var(--bb-space-1);
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
    .sheen {
      animation: none;
      background: none;
      -webkit-text-fill-color: currentColor;
    }
  }
</style>
