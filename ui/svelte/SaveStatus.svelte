<script module lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  // Per-row save/propagation indicator. The states mirror the real pipeline
  // rather than the form's own lifecycle, which is the whole point of the
  // element: "Saved" and "live in chat" are ~2.5 seconds apart here (a
  // write-behind flush plus a projector hop), and a row that says Saved while
  // the bot is still answering with the old text is the support ticket this
  // exists to prevent.
  //
  //   saving : form round trip in flight
  //   saved  : service accepted the write (write-behind flush pending)
  //   live   : past the write-behind + projector hop; synced to chat
  //   error  : write rejected; row rolled back
  export type SaveState = 'idle' | 'saving' | 'saved' | 'live' | 'error';
</script>

<script lang="ts">
  // Svelte adapter over the shared tag vocabulary (../styles/tags.css) plus the
  // one rule in ../styles/elements/save-status.css. Astro twin:
  // ../astro/SaveStatus.astro.
  //
  // A pure composition, and it should stay one. It was four bespoke animations
  // — a 50%-radius dot per state, sine-pulsing at 900ms while saving and
  // 1400ms once saved, with a green box-shadow glow on live. The motion is now
  // the travelling hairline (.bb-sweep) on the two states genuinely in motion.
  //
  // EVERY LABEL IS A PROP: the console localises and this package holds no
  // copy. The defaults are English so an unlocalised surface still renders
  // something readable rather than an empty tag.
  import '../styles/elements/save-status.css';

  let {
    state = 'idle',
    compact = false,
    savingLabel = 'Saving…',
    savedLabel = 'Saved',
    liveLabel = 'Synced to chat',
    errorLabel = 'Failed',
    class: className = '',
    ...rest
  }: {
    state?: SaveState;
    /** Mark only, no label: for a row too narrow to carry both. */
    compact?: boolean;
    savingLabel?: string;
    savedLabel?: string;
    liveLabel?: string;
    errorLabel?: string;
    class?: string;
    [key: string]: unknown;
  } = $props();

  // Tone + mark per state. saving/saved sit in the tan half of the palette,
  // live is the green label, error carries its own red tone.
  const TONE: Record<SaveState, string> = {
    idle: '',
    saving: 'bb-tag--quiet',
    saved: 'bb-tag--alpha',
    live: 'bb-tag--live',
    error: 'bb-tag--error',
  };
  const MARK: Record<SaveState, string> = {
    idle: '',
    saving: 'bb-mark--dash',
    saved: '',
    live: '',
    error: 'bb-mark--hollow',
  };

  const labels: Record<SaveState, string> = $derived({
    idle: '',
    saving: savingLabel,
    saved: savedLabel,
    live: liveLabel,
    error: errorLabel,
  });

  const classes = $derived(
    ['bb-tag', TONE[state] || null, className || null].filter(Boolean).join(' '),
  );
</script>

{#if state !== 'idle'}
  <span class={classes} role="status" {...rest}><i
      class={['bb-mark', MARK[state] || null].filter(Boolean).join(' ')}
      aria-hidden="true"
    ></i>{#if !compact}{labels[state]}{/if}{#if state === 'saving'}<i
      class="bb-sweep bb-sweep--tan"
      aria-hidden="true"
    ></i>{/if}{#if state === 'live'}<i class="bb-sweep" aria-hidden="true"></i>{/if}</span
  >
{/if}
