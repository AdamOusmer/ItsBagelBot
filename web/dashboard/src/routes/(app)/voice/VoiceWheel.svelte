<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  import { untrack } from 'svelte';
  import { getI18n } from '@bagel/kit/i18n/context';
  import Icon from '@bagel/ui/svelte/Icon.svelte';
  import { voiceOptions } from '#lib/components/voice/voice-options';

  let { voice, playing, onSelect, onPlay }: {
    voice: string;
    playing: string | null;
    onSelect: (voice: string) => void;
    onPlay: (voice: string) => void;
  } = $props();

  const ROW_PX = 44;
  const SETTLE_MS = 260;
  const MOVES: Record<string, number> = { ArrowDown: 1, ArrowUp: -1 };

  const { t } = getI18n();
  const options = voiceOptions(t);
  const span = options.length;
  const items = [0, 1, 2].flatMap((copy) => options.map((option) => ({ value: option.value, copy })));

  let wheel = $state<HTMLDivElement>();
  let center = $state(untrack(() => span + Math.max(0, options.findIndex((option) => option.value === voice))));
  let position = $state(untrack(() => center));
  let settle: ReturnType<typeof setTimeout> | undefined;
  const focused = $derived(items[center].value);

  $effect(() => {
    if (wheel) wheel.scrollTop = untrack(() => center) * ROW_PX;
    return () => clearTimeout(settle);
  });

  const inMiddleCopy = (index: number) => index >= span && index < span * 2;

  function recenter() {
    if (!wheel || inMiddleCopy(center)) return;
    wheel.scrollTop += (center < span ? span : -span) * ROW_PX;
  }

  function onscroll() {
    if (!wheel) return;
    position = wheel.scrollTop / ROW_PX;
    center = Math.min(items.length - 1, Math.max(0, Math.round(position)));
    clearTimeout(settle);
    settle = setTimeout(() => {
      recenter();
      if (focused !== voice) onSelect(focused);
    }, SETTLE_MS);
  }

  function scrollTo(index: number) {
    wheel?.scrollTo({ top: index * ROW_PX, behavior: 'smooth' });
  }

  function onkeydown(event: KeyboardEvent) {
    if (event.key === 'Enter' || event.key === ' ') {
      event.preventDefault();
      onPlay(focused);
      return;
    }
    if (!(event.key in MOVES)) return;
    event.preventDefault();
    scrollTo(Math.min(items.length - 1, Math.max(0, center + MOVES[event.key])));
  }

  const itemStyle = (index: number) => {
    const distance = Math.abs(index - position);
    return `opacity: ${Math.max(0.12, 1 - distance * 0.32)}; transform: scale(${Math.max(0.72, 1 - distance * 0.11)})`;
  };
</script>

<div class="wheel" bind:this={wheel} role="listbox" tabindex="0" aria-label={t('voice.choose')}
  aria-activedescendant="voice-wheel-1-{focused}" {onscroll} {onkeydown}>
  {#each items as item, i (`${item.copy}-${item.value}`)}
    <button type="button" id="voice-wheel-{item.copy}-{item.value}" role="option" aria-selected={i === center}
      aria-hidden={item.copy !== 1} tabindex="-1" class="item" class:center={i === center} style={itemStyle(i)}
      onclick={() => (i === center ? onPlay(item.value) : scrollTo(i))}>
      <span class="dot" class:on={item.value === voice}></span>
      <span class="name">{item.value}</span>
      <span class="play" aria-hidden="true"><Icon name={playing === item.value ? 'stop' : 'play'} /></span>
    </button>
  {/each}
</div>

<style>
  .wheel {
    --row: 44px;
    block-size: calc(var(--row) * 5);
    overflow-y: auto;
    scroll-snap-type: y mandatory;
    scrollbar-width: none;
    padding-block: calc(var(--row) * 2);
    mask-image: linear-gradient(to bottom, transparent, black 30%, black 70%, transparent);
    outline: none;
    border-radius: var(--bb-radius-md);
  }
  .wheel::-webkit-scrollbar { display: none; }
  .wheel:focus-visible { box-shadow: 0 0 0 2px rgba(var(--bb-tan-rgb), 0.5); }
  .item {
    --bb-cursor: quiet;
    display: grid;
    grid-template-columns: minmax(0, 1fr) auto minmax(0, 1fr);
    align-items: center;
    gap: var(--bb-space-3);
    inline-size: 100%;
    block-size: var(--row);
    scroll-snap-align: center;
    padding: 0;
    border: 0;
    background: none;
    color: var(--bb-muted);
    font: inherit;
    cursor: pointer;
  }
  .name { grid-column: 2; font-size: 21px; font-weight: 500; letter-spacing: -0.01em; }
  .center { color: var(--bb-text); }
  .center .name { font-size: 25px; font-weight: 600; }
  .dot {
    grid-column: 1;
    justify-self: end;
    inline-size: 6px;
    block-size: 6px;
    border-radius: 50%;
    background: var(--bb-green-glow);
    opacity: 0;
    transition: opacity var(--bb-dur-fast) var(--bb-ease-out-expo);
  }
  .dot.on { opacity: 1; }
  .play {
    grid-column: 3;
    justify-self: start;
    display: inline-flex;
    inline-size: 16px;
    block-size: 16px;
    opacity: 0;
    transition: opacity var(--bb-dur-fast) var(--bb-ease-out-expo);
  }
  .play :global(svg) { inline-size: 100%; block-size: 100%; }
  .center .play { opacity: 0.7; }
  .center:hover .play { opacity: 1; }
</style>
