<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import '../styles/elements/editor-stage.css';
  import type { Snippet } from 'svelte';
  import type { HTMLAttributes } from 'svelte/elements';

  type Own = {
    label?: string;
    railLabel?: string;
    topInset?: string;
    height?: string;
    bottomRoom?: string;
    topStart?: Snippet;
    topCenter?: Snippet;
    topEnd?: Snippet;
    slimBar?: boolean;
    rail?: Snippet;
    leftPanel?: Snippet;
    rightPanel?: Snippet;
    children: Snippet;
    onboardpointerdown?: (event: PointerEvent) => void;
    editorEl?: HTMLDivElement;
    barEl?: HTMLElement;
    railEl?: HTMLElement;
    boardEl?: HTMLDivElement;
    leftPanelEl?: HTMLDivElement;
    rightPanelEl?: HTMLDivElement;
    class?: string;
  };

  let {
    label,
    railLabel = 'Tools',
    topInset,
    height = '100%',
    bottomRoom,
    topStart,
    topCenter,
    topEnd,
    slimBar = false,
    rail,
    leftPanel,
    rightPanel,
    children,
    onboardpointerdown,
    editorEl = $bindable<HTMLDivElement | undefined>(undefined),
    barEl = $bindable<HTMLElement | undefined>(undefined),
    railEl = $bindable<HTMLElement | undefined>(undefined),
    boardEl = $bindable<HTMLDivElement | undefined>(undefined),
    leftPanelEl = $bindable<HTMLDivElement | undefined>(undefined),
    rightPanelEl = $bindable<HTMLDivElement | undefined>(undefined),
    class: className = '',
    ...rest
  }: Own & Omit<HTMLAttributes<HTMLDivElement>, keyof Own> = $props();

  const classes = $derived(
    [
      'bb-editor-stage',
      slimBar ? 'bb-editor-stage--slim-bar' : null,
      rail ? 'bb-editor-stage--has-rail' : null,
      className || null,
    ]
      .filter(Boolean)
      .join(' '),
  );
</script>

<div
  class={[classes, 'bb-canvas-fill']}
  style:--stage-top={topInset}
  style:--stage-height={height}
  style:--stage-bottom-room={bottomRoom}
  bind:this={editorEl}
  {...rest}
>
  <header class="bb-editor-stage__bar" bind:this={barEl}>
    {#if topStart}
      <div class="bb-editor-stage__bar-start">{@render topStart()}</div>
    {/if}
    {#if topCenter}
      <div class="bb-editor-stage__bar-center">{@render topCenter()}</div>
    {/if}
    <div class="bb-editor-stage__bar-end">{@render topEnd?.()}</div>
  </header>

  <div class="bb-editor-stage__body">
    {#if rail}
      <nav class="bb-editor-stage__rail" aria-label={railLabel} bind:this={railEl}>{@render rail()}</nav>
    {/if}

    <div
      class="bb-editor-stage__board"
      role="presentation"
      aria-label={label}
      bind:this={boardEl}
      onpointerdowncapture={onboardpointerdown}
    >
      {@render children()}

      {#if leftPanel}
        <div class="bb-editor-stage__panel bb-editor-stage__panel--left" bind:this={leftPanelEl}>
          <span class="bb-editor-stage__grip" aria-hidden="true"></span>
          {@render leftPanel()}
        </div>
      {/if}

      {#if rightPanel}
        <div class="bb-editor-stage__panel bb-editor-stage__panel--right" bind:this={rightPanelEl}>
          <span class="bb-editor-stage__grip" aria-hidden="true"></span>
          {@render rightPanel()}
        </div>
      {/if}
    </div>
  </div>
</div>
