<script lang="ts">
  // One ledger line in the counters deck, built on the shared ManagementRow so
  // the clickable primary is a real button and the delete quick-action is a
  // SIBLING of it, never nested inside — the same structure every other
  // management deck (commands, timers, rewards) uses, which is what keeps the
  // columns aligned across decks instead of drifting in a bespoke grid.
  //
  // Counters have no enable/disable in the loyalty service, so there is no
  // toggle switch; the row's "state" is its scope + value, both spelled as
  // text. A channel/bot counter shows its single tally on the right; the entry
  // scopes keep per-bucket values in the inspector, so the row states which
  // kind it is there instead.
  import { Icon, ManagementRow, getI18n, type CounterDef, type CounterScope } from '@bagel/shared';

  const { t } = getI18n();

  let {
    counter,
    index = undefined as number | undefined,
    expanded = false,
    onExpand,
    onDelete
  }: {
    counter: CounterDef;
    index?: number;
    expanded?: boolean;
    onExpand: () => void;
    onDelete: () => void;
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

  // The entry scopes keep no single value; the row states which kind it is
  // where a channel counter shows its tally, so the right column never reads
  // as an empty or zero value.
  const perScopeNote = $derived(c.scope === 'command' ? t('counters.perCommandNote') : t('counters.perUserNote'));
</script>

<ManagementRow selected={expanded} {expanded} controls="counter-inspector" onselect={onExpand}>
  {#snippet primary()}
    <span class="prow">
      {#if idx}<span class="idx" aria-hidden="true">{idx}</span>{/if}
      <span class="name">
        <span class="c-name">{c.name}</span>
        <span class="c-tag">{scopeLabel}</span>
      </span>
      <span class="meta">
        {#if isChannel}
          <span class="m-val">
            <span class="sr-only">{t('counters.colValue')} </span>{c.value.toLocaleString()}
          </span>
        {:else}
          <span class="m-note">{perScopeNote}</span>
        {/if}
      </span>
    </span>
  {/snippet}
  {#snippet actions()}
    <button class="mini" type="button" aria-label={t('counters.deleteAria', { name: c.name })} onclick={onDelete}>
      <Icon name="trash" size={15} />
    </button>
  {/snippet}
</ManagementRow>

<style>
  /* idx | name (+ scope tag) | value/note — the TimerRow track shape, so the
     value column lands at the same right edge on every row regardless of scope. */
  .prow {
    display: grid;
    grid-template-columns: 28px minmax(0, 1fr) auto;
    align-items: center;
    gap: 14px;
  }
  .idx { font-family: var(--bb-font-mono); font-size: 10px; color: var(--bb-muted); opacity: 0.55; }

  .name { display: inline-flex; align-items: center; gap: 10px; min-width: 0; }
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
    flex: none;
    font-family: var(--bb-font-body);
    font-size: 11px;
    color: var(--bb-muted);
    border: 1px solid var(--bb-border);
    border-radius: 999px;
    padding: 2px 8px;
    white-space: nowrap;
  }

  /* Value / note: right-aligned, the value mono + tabular so digits column
     across rows, the note muted where a value would be. */
  .meta { display: inline-flex; align-items: center; justify-content: flex-end; }
  .m-val {
    font-family: var(--bb-font-mono);
    font-size: 13.5px;
    color: var(--bb-tan-light);
    white-space: nowrap;
    font-variant-numeric: tabular-nums;
  }
  .m-note { font-family: var(--bb-font-body); font-size: 11px; color: var(--bb-muted); white-space: nowrap; }

  .mini {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 32px;
    height: 32px;
    border: 1px solid transparent;
    border-radius: 8px;
    background: none;
    color: var(--bb-muted);
    cursor: pointer;
  }
  .mini:hover { color: #cf8a78; border-color: rgba(176, 90, 70, 0.4); }
  .mini:focus-visible { outline: 2px solid var(--bb-green-glow, #52b788); outline-offset: 2px; }

  @media (max-width: 760px) {
    .prow {
      grid-template-columns: minmax(0, 1fr);
      grid-template-areas:
        'name'
        'meta';
      row-gap: 4px;
    }
    .idx { display: none; }
    .name { grid-area: name; }
    .meta { grid-area: meta; justify-content: flex-start; }
    .mini { min-width: 44px; min-height: 44px; }
  }
</style>
