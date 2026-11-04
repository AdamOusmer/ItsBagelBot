<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import { PREVIEW_KINDS, type FlowPreview } from '#lib/flow/thumb';
  import { wirePath } from '#lib/flow/wire';

  let { preview }: { preview: FlowPreview } = $props();

  const BOX_W = 12;
  const PAD = 2;
  const SECOND_PORT = 0.82;
  const FIRST_PORT = 0.6;

  const boxes = $derived(preview.n.map(([x, y, h, kind]) => ({ x, y, h, kind: PREVIEW_KINDS[kind] })));
  const width = $derived(Math.max(BOX_W, ...boxes.map((b) => b.x + BOX_W)));
  const height = $derived(Math.max(4, ...boxes.map((b) => b.y + b.h)));

  function wire([fromIndex, toIndex, second]: FlowPreview['e'][number]): string {
    const from = boxes[fromIndex];
    const to = boxes[toIndex];
    const out = from.kind === 'condition' ? (second ? SECOND_PORT : FIRST_PORT) : 0.5;
    const x1 = from.x + BOX_W;
    const y1 = from.y + from.h * out;
    const y2 = to.y + to.h / 2;
    return wirePath({ x: x1, y: y1 }, { x: to.x, y: y2 }, { turn: 1.2, radius: 0.6 });
  }
</script>

<svg
  class="thumb"
  viewBox="{-PAD} {-PAD} {width + PAD * 2} {height + PAD * 2}"
  preserveAspectRatio="xMidYMid meet"
  aria-hidden="true"
>
  {#each preview.e as e, i (i)}
    <path class="wire" class:no={e[2] === 1} d={wire(e)} />
  {/each}
  {#each boxes as b, i (i)}
    <rect class="box" data-kind={b.kind} x={b.x} y={b.y} width={BOX_W} height={b.h} rx="0.7" />
  {/each}
</svg>

<style>
  .thumb {
    display: block;
    width: 100%;
    height: var(--thumb-h, 112px);
  }
  .wire {
    fill: none;
    stroke: var(--bb-green-glow);
    stroke-width: 0.35;
    opacity: 0.7;
  }
  .wire.no {
    stroke: rgba(var(--bb-white-rgb), 0.5);
  }
  .box {
    fill: rgba(var(--bb-white-rgb), 0.14);
  }
  .box[data-kind='trigger'] {
    fill: rgba(var(--bb-green-glow-rgb), 0.45);
  }
  .box[data-kind='condition'] {
    fill: rgba(var(--bb-tan-rgb), 0.45);
  }
</style>
