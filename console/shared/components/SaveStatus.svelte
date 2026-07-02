<script module lang="ts">
  // Per-row save/propagation indicator. States mirror the real pipeline:
  //   saving — form round trip in flight
  //   saved  — service accepted the write (write-behind flush pending)
  //   live   — past the ~2.5s write-behind + projector hop; synced to chat
  //   error  — write rejected; row rolled back
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
</script>

{#if state !== 'idle'}
  <span class="save-status {state}" role="status">
    <span class="dot"></span>
    {#if !compact}<span class="label">{LABELS[state]}</span>{/if}
  </span>
{/if}

<style>
  .save-status {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    font-family: var(--bb-font-display);
    font-weight: 700;
    font-size: 11px;
    letter-spacing: 0.02em;
    white-space: nowrap;
  }

  .dot {
    width: 7px;
    height: 7px;
    border-radius: 50%;
    flex: none;
  }

  .saving { color: var(--bb-muted); }
  .saving .dot { background: var(--bb-muted); animation: pulse 900ms ease-in-out infinite; }

  .saved { color: var(--bb-tan-light); }
  .saved .dot { background: var(--bb-tan, #c9a87c); animation: pulse 1400ms ease-in-out infinite; }

  .live { color: var(--bb-green-glow); }
  .live .dot { background: var(--bb-green-glow, #52b788); box-shadow: 0 0 8px rgba(82, 183, 136, 0.6); }

  .error { color: #cf8a78; }
  .error .dot { background: #b05a46; }

  @keyframes pulse {
    0%, 100% { opacity: 1; }
    50% { opacity: 0.35; }
  }
</style>
