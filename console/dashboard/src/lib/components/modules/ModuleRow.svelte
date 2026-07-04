<script lang="ts">
  // One ledger line in the module deck — the module twin of CommandRow.
  // Selecting a row loads its config into the page's docked inspector; the row
  // itself owns only the quick enable/disable toggle. The page passes the
  // enhance handler so all optimistic-UI state lives in one place.
  import { enhance } from '$app/forms';
  import type { SubmitFunction } from '@sveltejs/kit';
  import { Icon, SaveStatus, getI18n, type ModuleState } from '@bagel/shared';
  import type { SaveState } from '@bagel/shared/components/SaveStatus.svelte';

  const { t } = getI18n();

  let {
    module: mod,
    index = undefined as number | undefined,
    status = 'idle' as SaveState,
    expanded = false,
    onExpand,
    toggleSubmit
  }: {
    module: ModuleState;
    index?: number;
    status?: SaveState;
    expanded?: boolean;
    onExpand: () => void;
    toggleSubmit: SubmitFunction;
  } = $props();

  const m = $derived(mod);
  const idx = $derived(index !== undefined ? String(index).padStart(2, '0') : '');

  function rowKey(e: KeyboardEvent) {
    if (e.key === 'Enter' || e.key === ' ') {
      e.preventDefault();
      onExpand();
    }
  }
</script>

<div
  class="row-shell reveal {expanded ? 'selected' : ''} {m.enabled ? '' : 'off'}"
  class:flash-save={status === 'saved'}
  style="--i: {(index ?? 1) - 1}"
>
  <!-- svelte-ignore a11y_no_static_element_interactions -->
  <div class="trow" role="button" tabindex="0" aria-pressed={expanded} onclick={onExpand} onkeydown={rowKey}>
    {#if idx}<span class="idx" aria-hidden="true">{idx}</span>{/if}
    <span class="mod-icon"><Icon name={m.def.icon} size={16} /></span>
    <span class="cmd">
      <span class="mod-label">{m.def.label}</span>
      <span class="mod-tag">{m.def.tagline}</span>
    </span>
    <span class="state"><SaveStatus state={status} /></span>
    <!-- svelte-ignore a11y_click_events_have_key_events a11y_no_static_element_interactions -->
    <span class="row-act" onclick={(e) => e.stopPropagation()}>
      <form method="POST" action="?/toggle" use:enhance={toggleSubmit}>
        <input type="hidden" name="name" value={m.def.id} />
        <input type="hidden" name="config" value={JSON.stringify(m.config)} />
        <input type="hidden" name="is_enabled" value={m.enabled ? '' : 'on'} />
        <button class="toggle {m.enabled ? 'on' : ''}" type="submit" aria-label={t('modules.toggleAria', { label: m.def.label })}></button>
      </form>
    </span>
  </div>
</div>

<style>
  /* Ledger line: identical rhythm to CommandRow — hairline baselines, index,
     tan left-rule selection, no rounded shells. */
  .row-shell {
    border-bottom: 1px solid var(--rule, rgba(240, 236, 228, 0.08));
    border-left: 1px solid transparent;
    transition: background var(--bb-dur-fast, 140ms) ease, border-color var(--bb-dur-fast, 140ms) ease;
  }
  .row-shell.selected {
    border-left-color: var(--bb-tan);
    background: rgba(201, 168, 124, 0.05);
  }
  .row-shell.off .trow > :not(.row-act) { opacity: 0.5; }

  .trow {
    display: grid;
    grid-template-columns: 28px 34px minmax(0, 1fr) auto auto;
    align-items: center;
    gap: 14px;
    padding: 14px 14px;
    cursor: pointer;
    user-select: none;
  }
  .trow:hover { background: rgba(201, 168, 124, 0.045); }
  .trow:focus-visible { outline: 1px solid var(--bb-tan, #c9a87c); outline-offset: -1px; }

  .idx {
    font-family: var(--bb-font-mono);
    font-size: 10px;
    color: var(--bb-muted);
    opacity: 0.55;
  }
  .row-shell.selected .idx { color: var(--bb-tan); opacity: 1; }

  .mod-icon {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 34px;
    height: 34px;
    border-radius: var(--bb-radius-md, 10px);
    background: rgba(201, 168, 124, 0.12);
    border: 1px solid var(--glass-border);
    color: var(--bb-tan-light);
  }

  .cmd { display: flex; flex-direction: column; gap: 3px; min-width: 0; }
  .mod-label { font-family: var(--bb-font-display); font-weight: 700; font-size: 15px; color: var(--bb-white); }
  .mod-tag {
    font-family: var(--bb-font-body);
    font-size: 12.5px;
    color: var(--bb-muted);
    line-height: 1.4;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    min-width: 0;
  }

  .state { min-width: 0; }
  .row-act { display: inline-flex; align-items: center; gap: 6px; }

  @media (max-width: 760px) {
    .trow {
      grid-template-columns: 34px minmax(0, 1fr) auto;
      grid-template-areas: 'icon text act';
      row-gap: 4px;
    }
    .idx { display: none; }
    .mod-icon { grid-area: icon; }
    .cmd { grid-area: text; }
    .mod-tag { white-space: normal; }
    .state { display: none; }
    .row-act { grid-area: act; }
    .row-act form { display: flex; align-items: center; justify-content: center; min-width: 44px; min-height: 44px; }
  }
</style>
