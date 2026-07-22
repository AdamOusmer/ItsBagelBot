<script lang="ts">
  // One ledger line in the counters deck, rendered as a non-interactive <li>.
  // Inside it sit SEPARATE controls, never nested: a disclosure button (opens
  // the row in the page inspector), a channel-scope +/- stepper (each button a
  // 44px target with a counter-specific accessible name), and a delete button.
  // Counters have no enable/disable in the loyalty service, so there is no
  // switch: the row's "state" is its scope + current value, both spelled as text.
  import { MiniButton, SaveStatus, getI18n, type CounterDef, type CounterScope } from '@bagel/shared';
  import type { SaveState } from '@bagel/shared/components/SaveStatus.svelte';

  const { t } = getI18n();

  let {
    counter,
    index = undefined as number | undefined,
    status = 'idle' as SaveState,
    expanded = false,
    onExpand,
    onDelete,
    onIncrement,
    onDecrement
  }: {
    counter: CounterDef;
    index?: number;
    status?: SaveState;
    expanded?: boolean;
    onExpand: () => void;
    onDelete: () => void;
    onIncrement: () => void;
    onDecrement: () => void;
  } = $props();

  const c = $derived(counter);
  const idx = $derived(index !== undefined ? String(index).padStart(2, '0') : '');
  const isChannel = $derived(c.scope === 'channel');

  const SCOPE_KEY: Record<CounterScope, string> = {
    channel: 'counters.tagChannel',
    viewer: 'counters.tagViewer',
    command: 'counters.tagCommand',
    viewer_command: 'counters.tagViewerCommand'
  };
  const scopeLabel = $derived(t(SCOPE_KEY[c.scope]));
</script>

<li
  class="row-shell reveal {expanded ? 'selected' : ''}"
  class:flash-save={status === 'saved'}
  style="--i: {(index ?? 1) - 1}"
>
  <!-- Disclosure: the ONLY control that opens the inspector. Its accessible name
       is the bare counter name; scope + value inside are decorative detail. -->
  <button
    class="disclosure"
    type="button"
    aria-expanded={expanded}
    aria-controls="counter-inspector"
    aria-label={c.name}
    onclick={onExpand}
  >
    {#if idx}<span class="idx" aria-hidden="true">{idx}</span>{/if}
    <span class="c-name">{c.name}</span>
    <span class="c-tag">{scopeLabel}</span>
  </button>

  <!-- Metadata: labelled TEXT. Channel counters show their tally; entry-scoped
       counters keep per-bucket values, so the row states which kind instead. -->
  <div class="meta">
    {#if isChannel}
      <span class="m-item">
        <span class="m-lbl">{t('counters.colValue')}</span>
        <span class="m-val">{c.value.toLocaleString()}</span>
      </span>
    {:else if c.scope === 'command'}
      <span class="m-flag">{t('counters.perCommandNote')}</span>
    {:else}
      <span class="m-flag">{t('counters.perUserNote')}</span>
    {/if}
    <span class="m-state"><SaveStatus state={status} compact /></span>
  </div>

  <div class="row-act">
    {#if isChannel}
      <!-- Single-activation stepper: each click changes the tally by exactly one.
           The glyphs are decorative; the accessible name names the counter. -->
      <div class="stepper" role="group" aria-label={t('counters.stepperAria', { name: c.name })}>
        <button
          class="step"
          type="button"
          aria-label={t('counters.decreaseAria', { name: c.name })}
          onclick={onDecrement}
        ><span aria-hidden="true">−</span></button>
        <button
          class="step"
          type="button"
          aria-label={t('counters.increaseAria', { name: c.name })}
          onclick={onIncrement}
        ><span aria-hidden="true">+</span></button>
      </div>
    {/if}
    <MiniButton icon="trash" class="del" aria-label={t('counters.deleteAria', { name: c.name })} onclick={onDelete} />
  </div>
</li>

<style>
  .row-shell {
    list-style: none;
    display: grid;
    grid-template-columns: minmax(0, 1fr) auto auto;
    align-items: center;
    gap: 14px;
    padding: 0 14px 0 0;
    border-bottom: 1px solid var(--rule, rgba(240, 236, 228, 0.08));
    transition: background var(--bb-dur-fast, 140ms) ease, border-color var(--bb-dur-fast, 140ms) ease;
  }
  .row-shell.selected { background: rgba(201, 168, 124, 0.05); }

  .disclosure {
    display: grid;
    grid-template-columns: 28px minmax(0, 1fr) auto;
    align-items: center;
    gap: 14px;
    padding: 12px 0 12px 14px;
    min-width: 0;
    text-align: left;
    background: none;
    border: 0;
    color: inherit;
    font: inherit;
    cursor: pointer;
    user-select: none;
  }
  .disclosure:hover { background: rgba(201, 168, 124, 0.045); }
  .disclosure:focus-visible { outline: 1px solid var(--bb-tan, #c9a87c); outline-offset: -1px; }

  .idx { font-family: var(--bb-font-mono); font-size: 10px; color: var(--bb-muted); opacity: 0.55; }
  .row-shell.selected .idx { color: var(--bb-tan); opacity: 1; }

  .c-name {
    font-family: var(--bb-font-display);
    font-weight: 700;
    font-size: 14px;
    color: var(--bb-white);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    min-width: 0;
  }
  .c-tag {
    font-family: var(--bb-font-body);
    font-size: 11px;
    color: var(--bb-muted);
    border: 1px solid var(--bb-border);
    border-radius: 999px;
    padding: 2px 8px;
    white-space: nowrap;
  }

  /* Fixed column tracks (the CommandRow pattern) so the value/note and the
     save state line up across rows instead of shifting with content, which
     also keeps every row's stepper/delete at the same x position. */
  .meta {
    display: grid;
    grid-template-columns: 132px 48px;
    align-items: center;
    gap: 12px;
  }
  .meta > * { min-width: 0; }
  .m-item { display: inline-flex; align-items: baseline; gap: 5px; }
  .m-lbl {
    font-family: var(--bb-font-body);
    font-size: 10px;
    letter-spacing: 0.04em;
    text-transform: uppercase;
    color: var(--bb-muted);
    opacity: 0.7;
  }
  .m-val {
    font-family: var(--bb-font-mono);
    font-size: 13px;
    color: var(--bb-white);
    white-space: nowrap;
    font-variant-numeric: tabular-nums;
  }
  .m-flag { font-family: var(--bb-font-body); font-size: 11px; color: var(--bb-muted); }
  .m-state { display: inline-flex; align-items: center; min-width: 0; }

  /* Stepper: two 44px targets sharing one pill; the delete sits >=8px away. */
  .row-act { display: inline-flex; align-items: center; gap: 10px; }
  .stepper { display: inline-flex; align-items: center; border: 1px solid var(--bb-border); border-radius: 8px; overflow: hidden; }
  .step {
    width: 44px;
    height: 44px;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    background: none;
    border: 0;
    color: var(--bb-tan-light);
    font-size: 18px;
    line-height: 1;
    cursor: pointer;
    transition: background var(--bb-dur-fast, 140ms) ease, color var(--bb-dur-fast, 140ms) ease;
  }
  .step:first-child { border-right: 1px solid var(--bb-border); }
  .step:hover { background: rgba(201, 168, 124, 0.08); color: var(--bb-tan-pale); }
  .step:focus-visible { outline: 1px solid var(--bb-tan, #c9a87c); outline-offset: -2px; }
  .row-act :global(.del:hover) { color: #cf8a78; }

  @media (max-width: 760px) {
    .row-shell {
      grid-template-columns: minmax(0, 1fr) auto;
      grid-template-areas:
        'disc act'
        'meta act';
      column-gap: 8px;
      row-gap: 4px;
      padding: 4px 12px;
    }
    .disclosure { grid-area: disc; grid-template-columns: 1fr auto; gap: 8px; padding: 8px 0 4px; }
    .idx { display: none; }
    .meta {
      grid-area: meta;
      grid-template-columns: auto auto;
      justify-content: start;
      gap: 8px 12px;
      padding-bottom: 8px;
    }
    .row-act { grid-area: act; }
  }
</style>
