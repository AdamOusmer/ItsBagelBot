<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  // One tone dot for every admin status line. It sits in $lib/components rather
  // than under a page folder because the overview's health panel and the users
  // inspector's token/EventSub rows need the same mark, and a per-page copy is
  // exactly the sibling-shaped duplicate that let the old `.probe-dot` and
  // `.nd` drift into two different greens.
  //
  // Not in @bagel/kit: the dashboard has no equivalent surface, and the tone
  // VOCABULARY is already shared (statusTone). This is only its paint.
  //
  // aria-hidden, always: the dot is decoration on top of a state that the text
  // beside it already names. Colour is never the only signal.
  import type { StatusTone } from '@bagel/kit/status-tone';

  let { tone }: { tone: StatusTone } = $props();
</script>

<span class="dot {tone}" aria-hidden="true"></span>

<style>
  .dot {
    width: 8px;
    height: 8px;
    border-radius: 50%;
    flex: none;
  }
  .dot.success {
    background: var(--bb-green-glow);
    box-shadow: 0 0 8px var(--bb-green-glow);
  }
  .dot.warning {
    background: var(--bb-tan);
    box-shadow: 0 0 8px var(--bb-tan);
  }
  .dot.error {
    background: var(--bb-status-error);
    box-shadow: 0 0 8px var(--bb-status-error);
  }
  .dot.neutral {
    background: transparent;
    border: 1px solid var(--bb-border-strong);
  }
</style>
