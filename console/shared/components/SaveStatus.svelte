<script module lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  // Per-row save/propagation indicator. States mirror the real pipeline:
  //   saving : form round trip in flight
  //   saved  : service accepted the write (write-behind flush pending)
  //   live   : past the ~2.5s write-behind + projector hop; synced to chat
  //   error  : write rejected; row rolled back
  export type SaveState = 'idle' | 'saving' | 'saved' | 'live' | 'error';
</script>

<script lang="ts">
  let { state = 'idle' as SaveState, compact = false } = $props();

  const LABELS: Record<Exclude<SaveState, 'idle'>, string> = {
    saving: 'Saving…',
    saved: 'Saved',
    live: 'Synced to chat',
    error: 'Failed'
  };

  // Tone + mark per state. saving/saved sit in the tan half of the palette,
  // live is the green label, error carries its own red tone below.
  const TONE: Record<SaveState, string> = {
    idle: '',
    saving: 'bb-tag--quiet',
    saved: 'bb-tag--alpha',
    live: 'bb-tag--live',
    error: 'is-error'
  };
  const MARK: Record<SaveState, string> = {
    idle: '',
    saving: 'bb-mark--dash',
    saved: '',
    live: '',
    error: 'bb-mark--hollow'
  };
</script>

{#if state !== 'idle'}
  <span class="bb-tag {TONE[state]}" role="status">
    <i class="bb-mark {MARK[state]}" aria-hidden="true"></i>
    {#if !compact}{LABELS[state]}{/if}
    {#if state === 'saving'}<i class="bb-sweep bb-sweep--tan" aria-hidden="true"></i>{/if}
    {#if state === 'live'}<i class="bb-sweep" aria-hidden="true"></i>{/if}
  </span>
{/if}

<style>
  /* Was a 50%-radius dot per state, sine-pulsing at 900ms while saving and
     1400ms once saved, with a green box-shadow glow on live. The motion is now
     the travelling hairline (.bb-sweep) on the two states that are actually in
     motion; the rest is the global label + rotated-square mark. */
  .is-error { color: #cf8a78; border-bottom-color: rgba(176,90,70,0.45); }
</style>
