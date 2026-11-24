<script module lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import type { Box } from '../lib/box-transform';

  export type CanvasBox = Box & { id: string; label: string; rotation?: number; hidden?: boolean; locked?: boolean };
</script>

<script lang="ts">
  import '../styles/elements/dot-canvas.css';
  import '../styles/elements/overlay-canvas.css';
  import type { Snippet } from 'svelte';
  import { clampBox, HANDLES, transformBox, type Guides, type Handle } from '../lib/box-transform';

  type Own = {
    width: number;
    height: number;
    boxes: readonly CanvasBox[];
    selected?: string;
    zoom?: number;
    handleLabels: Record<Exclude<Handle, 'move'>, string>;
    onselect?: (id: string) => void;
    onchange?: (id: string, box: Box) => void;
    oncommit?: () => void;
    oncancel?: () => void;
    preview?: Snippet<[CanvasBox, number]>;
    dots?: boolean;
    frameLabel?: string;
    tagSelected?: boolean;
    onscale?: (scale: number) => void;
    fitMargin?: number;
    class?: string;
  };

  type Drag = { box: CanvasBox; handle: Handle; pointer: number; x: number; y: number; others: readonly Box[] };

  const FIT_MARGIN = 24;
  const LABEL_ROOM = 20;
  const DOT_UNIT = 40;
  const DOT_MIN_GAP = 12;
  const SNAP_PIXELS = 7;
  const NUDGE = 1;
  const NUDGE_FAST = 10;
  const ARROWS: Record<string, [number, number]> = { ArrowLeft: [-1, 0], ArrowRight: [1, 0], ArrowUp: [0, -1], ArrowDown: [0, 1] };

  let {
    width,
    height,
    boxes,
    selected = '',
    zoom = 0,
    handleLabels,
    onselect,
    onchange,
    oncommit,
    oncancel,
    preview,
    dots = false,
    frameLabel,
    tagSelected = false,
    onscale,
    fitMargin = FIT_MARGIN,
    class: className = '',
  }: Own = $props();

  let drag: Drag | undefined;
  let guides = $state<Guides>({});
  let viewportWidth = $state(0);
  let viewportHeight = $state(0);

  const verticalMargin = $derived(FIT_MARGIN + (frameLabel ? LABEL_ROOM : 0));
  const scale = $derived(zoom || Math.max(0.01, Math.min((viewportWidth - fitMargin) / width, (viewportHeight - verticalMargin) / height)));

  const dotGap = $derived(DOT_UNIT * scale * 2 ** Math.max(0, Math.ceil(Math.log2(DOT_MIN_GAP / (DOT_UNIT * scale)))));

  $effect(() => onscale?.(scale));

  function begin(event: PointerEvent, box: CanvasBox, handle: Handle): void {
    if (event.button !== 0) return;
    event.stopPropagation();
    onselect?.(box.id);
    if (box.locked) return;
    (event.currentTarget as HTMLElement).setPointerCapture(event.pointerId);
    const others = boxes.filter((item) => item.id !== box.id && !item.hidden);
    drag = { box, handle, pointer: event.pointerId, x: event.clientX, y: event.clientY, others };
  }

  function move(event: PointerEvent): void {
    if (!drag || drag.pointer !== event.pointerId) return;
    const delta = { x: (event.clientX - drag.x) / scale, y: (event.clientY - drag.y) / scale };
    const motion = { handle: drag.handle, threshold: event.altKey ? 0 : SNAP_PIXELS / scale };
    const result = transformBox({ box: drag.box, others: drag.others, canvas: { width, height } }, delta, motion);
    guides = result.guides;
    onchange?.(drag.box.id, result.box);
  }

  function finish(event: PointerEvent): void {
    if (!drag || drag.pointer !== event.pointerId) return;
    drag = undefined;
    guides = {};
    if (event.type === 'pointercancel') oncancel?.();
    else oncommit?.();
  }

  function nudge(event: KeyboardEvent, box: CanvasBox): void {
    const step = ARROWS[event.key];
    if (!step || box.locked) return;
    event.preventDefault();
    const distance = event.shiftKey ? NUDGE_FAST : NUDGE;
    onchange?.(box.id, clampBox({ ...box, x: box.x + step[0] * distance, y: box.y + step[1] * distance }, { width, height }));
    oncommit?.();
  }
</script>

<div class={['bb-overlay-canvas', className]} bind:clientWidth={viewportWidth} bind:clientHeight={viewportHeight}>
  <div
    class={['bb-overlay-canvas__frame', dots && 'bb-dot-canvas']}
    style:width={`${width * scale}px`}
    style:height={`${height * scale}px`}
    style:background-size={dots ? `${dotGap}px ${dotGap}px` : undefined}
  >
    {#if frameLabel}<span class="bb-overlay-canvas__size">{frameLabel}</span>{/if}
    {#each boxes as box (box.id)}
      {#if !box.hidden}
        <div
          class="bb-overlay-canvas__box"
          class:is-selected={selected === box.id}
          class:is-locked={box.locked}
          style:left={`${box.x * scale}px`}
          style:top={`${box.y * scale}px`}
          style:width={`${box.w * scale}px`}
          style:height={`${box.h * scale}px`}
          style:transform={`rotate(${box.rotation ?? 0}deg)`}
        >
          {#if preview}<div class="bb-overlay-canvas__content">{@render preview(box, scale)}</div>{/if}
          <button
            type="button"
            class="bb-overlay-canvas__target"
            style="--bb-cursor: quiet"
            aria-label={box.label}
            title={box.label}
            aria-pressed={selected === box.id}
            onclick={() => onselect?.(box.id)}
            onkeydown={(event) => nudge(event, box)}
            onpointerdown={(event) => begin(event, box, 'move')}
            onpointermove={move}
            onpointerup={finish}
            onpointercancel={finish}
          ></button>
          {#if tagSelected && selected === box.id}<span class="bb-overlay-canvas__tag">{box.label}</span>{/if}
          {#if selected === box.id && !box.locked}
            {#each HANDLES as handle (handle)}
              <button
                type="button"
                class={['bb-overlay-canvas__handle', `bb-overlay-canvas__handle--${handle}`]}
                tabindex="-1"
                aria-label={handleLabels[handle]}
                onpointerdown={(event) => begin(event, box, handle)}
                onpointermove={move}
                onpointerup={finish}
                onpointercancel={finish}
              ></button>
            {/each}
          {/if}
        </div>
      {/if}
    {/each}
    {#if guides.x !== undefined}<div class="bb-overlay-canvas__guide bb-overlay-canvas__guide--x" style:left={`${guides.x * scale}px`}></div>{/if}
    {#if guides.y !== undefined}<div class="bb-overlay-canvas__guide bb-overlay-canvas__guide--y" style:top={`${guides.y * scale}px`}></div>{/if}
  </div>
</div>
