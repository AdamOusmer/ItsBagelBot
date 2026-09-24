<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import Bolota from '@bagel/kit/components/Bolota.svelte';

  type Sequence = 'entrance' | 'burst' | 'orbit' | 'comet';

  let {
    expression,
    sequence,
    sequenceKey,
    size = 200
  }: {
    expression: string;
    sequence: Sequence;
    sequenceKey: number;
    size?: number;
  } = $props();
</script>

<div class="mascot" aria-hidden="true">
  <div class="float">
    <div class="plate"></div>
    <Bolota
      name="ItsBagelBot"
      {size}
      active
      follow
      cycle={false}
      {expression}
      {sequence}
      {sequenceKey}
      sequenceFor={1100}
      sequenceHold={240}
    />
  </div>
</div>

<style>
  .mascot {
    display: flex;
    justify-content: center;
    padding: 18px 0 8px;
  }
  .float {
    position: relative;
    filter: drop-shadow(0 18px 34px rgba(0, 0, 0, 0.42));
    animation: float 9s ease-in-out infinite;
  }
  .plate {
    position: absolute;
    inset: -22%;
    z-index: -1;
    border-radius: 50%;
    background: radial-gradient(
      circle at 40% 35%,
      rgba(var(--bb-green-glow-rgb), 0.34),
      rgba(var(--bb-tan-rgb), 0.16) 46%,
      transparent 70%
    );
    filter: blur(22px);
    animation: breathe 6s ease-in-out infinite;
  }
  @keyframes float {
    0%,
    100% {
      transform: translate(0, 0);
    }
    32% {
      transform: translate(14px, -6px);
    }
    64% {
      transform: translate(-11px, 5px);
    }
  }
  @keyframes breathe {
    0%,
    100% {
      opacity: 0.7;
      transform: scale(1);
    }
    50% {
      opacity: 1;
      transform: scale(1.08);
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .float,
    .plate {
      animation: none;
    }
  }
</style>
