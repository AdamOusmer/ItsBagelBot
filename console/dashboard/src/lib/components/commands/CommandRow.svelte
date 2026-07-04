<script lang="ts">
  // One ledger line in the command deck. Selecting a row loads it into the
  // page's inspector pane; the row itself owns only the quick actions
  // (toggle, delete) — the page passes the enhance handlers so all
  // optimistic-UI state lives in one place.
  import { enhance } from '$app/forms';
  import type { SubmitFunction } from '@sveltejs/kit';
  import { Icon, Badge, SaveStatus, getI18n, type CommandView, type Perm } from '@bagel/shared';
  import type { SaveState } from '@bagel/shared/components/SaveStatus.svelte';

  const { t } = getI18n();

  let {
    command,
    index = undefined as number | undefined,
    status = 'idle' as SaveState,
    unsaved = false,
    expanded = false,
    onExpand,
    onDelete,
    toggleSubmit
  }: {
    command: CommandView;
    index?: number;
    status?: SaveState;
    unsaved?: boolean;
    expanded?: boolean;
    onExpand: () => void;
    onDelete: () => void;
    toggleSubmit: SubmitFunction;
  } = $props();

  const c = $derived(command);
  const cd = $derived(c.cooldown && c.cooldown > 0 ? `${c.cooldown}s` : '0s');
  const idx = $derived(index !== undefined ? String(index).padStart(2, '0') : '');

  function rowKey(e: KeyboardEvent) {
    if (e.key === 'Enter' || e.key === ' ') {
      e.preventDefault();
      onExpand();
    }
  }
</script>

<div
  class="row-shell reveal {expanded ? 'selected' : ''} {c.is_active ? '' : 'off'}"
  class:flash-save={status === 'saved'}
  style="--i: {(index ?? 1) - 1}"
>
  <!-- svelte-ignore a11y_no_static_element_interactions -->
  <div
    class="trow"
    role="button"
    tabindex="0"
    aria-pressed={expanded}
    onclick={onExpand}
    onkeydown={rowKey}
  >
    {#if idx}<span class="idx" aria-hidden="true">{idx}</span>{/if}
    <span class="cmd">
      <span class="cmd-name">
        !{c.name}
        {#if c.allowed_user_id}
          <span class="lock" title={t('commandRow.lockedTo', { id: c.allowed_user_id })}><Icon name="lock" size={11} /></span>
        {/if}
        {#if c.stream_online_only}
          <span class="lock" title={t('commandRow.liveOnly')}><Icon name="pulse" size={11} /></span>
        {/if}
        {#if c.builtin}
          <span class="builtin-tag" title={t('commandRow.builtinTitle')}>{t('commandRow.builtin')}</span>
        {/if}
        {#if unsaved}
          <span class="unsaved" title={t('commandRow.unsavedTitle')}>{t('commandRow.unsaved')}</span>
        {/if}
      </span>
      {#if c.aliases?.length}
        <span class="aliases" title={t('commandRow.also', { aliases: c.aliases.join(', ') })}>
          {#each c.aliases as a}<span class="alias-tag">{a}</span>{/each}
        </span>
      {/if}
    </span>
    <span class="resp">{c.response}</span>
    <span class="meta">
      <Badge perm={(c.perm ?? 'everyone') as Perm} />
      <span class="cd" title={t('commandRow.cooldown')}>{cd}</span>
      <span class="uses" title={t('commandRow.uses')}>{c.uses ?? '0'}</span>
    </span>
    <span class="state"><SaveStatus state={status} /></span>
    <!-- svelte-ignore a11y_click_events_have_key_events a11y_no_static_element_interactions -->
    <span class="row-act" onclick={(e) => e.stopPropagation()}>
      <!-- Toggle: silent upsert that flips is_active, preserving config -->
      <form method="POST" action={c.builtin ? '?/toggleBuiltin' : '?/toggle'} use:enhance={toggleSubmit}>
        <input type="hidden" name="name" value={c.name} />
        {#each c.aliases ?? [] as a}<input type="hidden" name="aliases" value={a} />{/each}
        <input type="hidden" name="response" value={c.response} />
        <input type="hidden" name="perm" value={c.perm ?? 'everyone'} />
        <input type="hidden" name="cooldown" value={c.cooldown ?? 0} />
        <input type="hidden" name="allowed_user_id" value={c.allowed_user_id ?? ''} />
        <input type="hidden" name="stream_online_only" value={c.stream_online_only ? 'on' : ''} />
        <input type="hidden" name="is_active" value={c.is_active ? '' : 'on'} />
        <button class="toggle {c.is_active ? 'on' : ''}" type="submit" aria-label={t('commandRow.toggleAria', { name: c.name })}></button>
      </form>
      {#if !c.builtin}
        <button class="mini" type="button" aria-label={t('commandRow.deleteAria', { name: c.name })} onclick={onDelete}>
          <Icon name="trash" size={15} />
        </button>
      {:else}
        <!-- Built-ins can't be deleted; hold the delete slot so the toggle stays
             column-aligned with the deletable custom rows. -->
        <span class="mini-spacer" aria-hidden="true"></span>
      {/if}
    </span>
  </div>
</div>

<style>
  /* Ledger line: hairline baselines, index number, selection keyed by a tan
     left rule + faint tint — no rounded shells or glows. */
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
    grid-template-columns: 28px minmax(130px, 1fr) minmax(0, 1.8fr) auto auto auto;
    align-items: center;
    gap: 14px;
    padding: 12px 14px;
    cursor: pointer;
    user-select: none;
    border-bottom: none;
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

  .cmd { display: flex; flex-direction: column; gap: 3px; min-width: 0; }
  .cmd-name {
    display: inline-flex; align-items: center; gap: 2px;
    font-family: var(--bb-font-mono); font-size: 13.5px; color: var(--bb-tan-light);
  }
  .lock { display: inline-flex; color: var(--bb-muted); margin-left: 6px; vertical-align: middle; }

  .unsaved {
    margin-left: 8px;
    font-family: var(--bb-font-display);
    font-weight: 700;
    font-size: 9.5px;
    letter-spacing: 0.02em;
    color: var(--bb-tan-light);
    border: 1px solid rgba(201, 168, 124, 0.4);
    border-radius: var(--bb-radius-pill, 100px);
    padding: 1px 8px;
  }

  .builtin-tag {
    margin-left: 8px;
    font-family: var(--bb-font-display);
    font-weight: 700;
    font-size: 9.5px;
    letter-spacing: 0.03em;
    text-transform: uppercase;
    color: var(--bb-green-glow, #7fd4a3);
    border: 1px solid rgba(82, 183, 136, 0.4);
    border-radius: var(--bb-radius-pill, 100px);
    padding: 1px 8px;
  }

  .aliases { display: flex; flex-wrap: wrap; gap: 4px; }
  .alias-tag {
    font-family: var(--bb-font-mono);
    font-size: 10px;
    color: var(--bb-muted);
    border: 1px solid var(--rule, rgba(240, 236, 228, 0.1));
    border-radius: 2px;
    padding: 1px 6px;
    white-space: nowrap;
  }

  .resp {
    font-family: var(--bb-font-body);
    font-size: 13px;
    color: var(--bb-muted);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    min-width: 0;
  }

  .meta { display: inline-flex; align-items: center; gap: 10px; }
  .cd, .uses {
    font-family: var(--bb-font-mono);
    font-size: 11.5px;
    color: var(--bb-muted);
    white-space: nowrap;
    font-variant-numeric: tabular-nums;
  }
  .uses { color: var(--bb-white); }

  .state { min-width: 0; }
  .row-act { display: inline-flex; align-items: center; gap: 6px; }
  .mini-spacer { width: 28px; height: 28px; flex: none; }

  @media (max-width: 760px) {
    .trow {
      grid-template-columns: minmax(0, 1fr) auto;
      grid-template-areas:
        'cmd act'
        'resp act'
        'meta act';
      row-gap: 4px;
    }
    .idx { display: none; }
    .cmd { grid-area: cmd; }
    /* Unprefixed line-clamp is NOT a drop-in for the -webkit- combo: in new
       Chromium it implies `continue: discard`, which collapses this box to
       display:none. Legacy -webkit- clamp only. */
    .resp { grid-area: resp; white-space: normal; display: -webkit-box; -webkit-line-clamp: 2; -webkit-box-orient: vertical; }
    .meta { grid-area: meta; }
    .state { display: none; }
    .row-act { grid-area: act; flex-direction: column; gap: 4px; }
    /* 44px touch target lives on the form wrapper; the switch keeps its
       natural 42x24 shape instead of being stretched into a tall box. */
    .row-act form {
      display: flex;
      align-items: center;
      justify-content: center;
      min-width: 44px;
      min-height: 44px;
    }
    .mini { min-width: 44px; min-height: 44px; }
    .mini-spacer { width: 44px; height: 44px; }
  }
</style>
