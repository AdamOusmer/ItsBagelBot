<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import { onMount, type Snippet } from 'svelte';
  import { fly } from 'svelte/transition';
  import '../styles/elements/page-hero.css';
  import '../styles/elements/setup-step.css';
  import { riseLines } from '../lib/line-rise';
  import { bezier } from '../lib/tween';
  import { prefersReducedMotion } from '../lib/motion-query';
  import Eyebrow from './Eyebrow.svelte';
  import Text from './Text.svelte';

  type Own = {
    kicker: string;
    title: string;
    body?: string;
    dir?: number;
    instant?: boolean;
    hero?: boolean;
    heading?: HTMLHeadingElement | null;
    class?: string;
    children?: Snippet;
    status?: Snippet;
  };

  let {
    kicker,
    title,
    body = '',
    dir = 1,
    instant = false,
    hero = false,
    heading = $bindable(null),
    class: className = '',
    children,
    status,
  }: Own = $props();

  const id = $props.id();
  const expo = bezier(0.16, 1, 0.3, 1);
  const OUT_MS = 280;
  const side = (i: number) => (i % 2 ? -1 : 1);
  const still = () => instant || prefersReducedMotion();
  const arrive = (node: Element, { i = 0 } = {}) =>
    still() ? { duration: 0 } : fly(node, { x: dir * 72 * side(i), duration: 760, delay: OUT_MS + 60 + i * 70, easing: expo, opacity: 0 });
  const depart = (node: Element, { i = 0 } = {}) =>
    still() ? { duration: 0 } : fly(node, { x: -dir * 56 * side(i), duration: OUT_MS - i * 20, delay: i * 20, easing: expo, opacity: 0 });
  let shown = $state(false);
  onMount(() => {
    if (!still() && heading) return riseLines(heading);
    shown = true;
  });
  const classes = $derived(['bb-setup-step', hero && 'bb-setup-step--hero', className || null].filter(Boolean).join(' '));
</script>

<section class={classes} aria-labelledby="{id}-title">
  <div class="bb-setup-step__kicker" in:arrive={{ i: 0 }} out:depart={{ i: 0 }}><Eyebrow as="p" tone="go">{kicker}</Eyebrow></div>
  <h1 id="{id}-title" class="bb-setup-step__title" class:is-shown={shown} tabindex="-1" bind:this={heading} out:depart={{ i: 1 }}><span data-fit="block" data-fit-ready>{title}</span></h1>
  <div class="bb-setup-step__body" in:arrive={{ i: 2 }} out:depart={{ i: 2 }}>{#if body}<Text tone="muted">{body}</Text>{/if}</div>
  <div class="bb-setup-step__controls" in:arrive={{ i: 3 }} out:depart={{ i: 3 }}>{@render children?.()}</div>
  <div class="bb-setup-step__status" data-reserve={status ? '' : undefined} in:arrive={{ i: 4 }} out:depart={{ i: 4 }}>{@render status?.()}</div>
</section>
