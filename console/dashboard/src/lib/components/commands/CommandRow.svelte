<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  // One ledger line in the command deck, on the shared ManagementRow: the
  // clickable primary is a real button and the quick actions (toggle, delete)
  // are siblings of it, never nested inside. The page passes the enhance
  // handlers so all optimistic-UI state lives in one place.
  import { enhance } from '$app/forms';
  import type { SubmitFunction } from '@sveltejs/kit';
  import { Icon, Badge, SaveStatus, ManagementRow, Switch, getI18n, usesCount, type CommandView, type Perm } from '@bagel/shared';
  import type { SaveState } from '@bagel/shared/components/SaveStatus.svelte';

  const { t } = getI18n();

  let {
    command,
    index = undefined as number | undefined,
    usesMax = 0,
    status = 'idle' as SaveState,
    unsaved = false,
    expanded = false,
    onExpand,
    onDelete,
    toggleSubmit
  }: {
    command: CommandView;
    index?: number;
    /** Largest use count in the visible deck; sets the bar's full width. */
    usesMax?: number;
    status?: SaveState;
    unsaved?: boolean;
    expanded?: boolean;
    onExpand: () => void;
    onDelete: () => void;
    toggleSubmit: SubmitFunction;
  } = $props();

  const c = $derived(command);
  const cd = $derived(c.cooldown && c.cooldown > 0 ? `${c.cooldown}s` : '\u2014');
  const idx = $derived(index !== undefined ? String(index).padStart(2, '0') : '');
  const uses = $derived(usesCount(c));
  // Share of the deck's busiest command. Clamped so a stale usesMax (a row
  // arriving from an optimistic save before the page re-derives the max) can
  // never draw a bar past its track.
  const barPct = $derived(usesMax > 0 ? Math.min(100, Math.round((uses / usesMax) * 100)) : 0);
</script>

<div class="row-wrap" class:flash-save={status === 'saved'}>
  <ManagementRow
    selected={expanded}
    {expanded}
    disabled={!c.is_active}
    onselect={onExpand}
  >
    {#snippet primary()}
      <span class="prow">
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
        <span class="m-perm"><Badge perm={(c.perm ?? 'everyone') as Perm} /></span>
        <span class="m-uses">
          <span class="u-line">
            <span class="m-val uses">{uses.toLocaleString()}</span>
            <span class="m-lbl">{t('commandRow.uses')}</span>
          </span>
          <!-- Relative-use bar: the deck ranks by use, so the row carries the
               proportion it is ranked on instead of leaving the reader to
               compare raw numbers down the column. -->
          <span class="u-track" aria-hidden="true">
            <span class="u-fill" style="width:{barPct}%"></span>
          </span>
        </span>
        <span class="m-cd">{cd}</span>
        <span class="state"><SaveStatus state={status} /></span>
      </span>
    {/snippet}
    {#snippet actions()}
      <form method="POST" action={c.builtin ? '?/toggleBuiltin' : '?/toggle'} use:enhance={toggleSubmit}>
        <input type="hidden" name="name" value={c.name} />
        {#each c.aliases ?? [] as a}<input type="hidden" name="aliases" value={a} />{/each}
        <input type="hidden" name="response" value={c.response} />
        <input type="hidden" name="perm" value={c.perm ?? 'everyone'} />
        <input type="hidden" name="cooldown" value={c.cooldown ?? 0} />
        <input type="hidden" name="allowed_user_id" value={c.allowed_user_id ?? ''} />
        <input type="hidden" name="stream_online_only" value={c.stream_online_only ? 'on' : ''} />
        <input type="hidden" name="is_active" value={c.is_active ? '' : 'on'} />
        <Switch type="submit" checked={c.is_active} label={t('commandRow.toggleAria', { name: c.name })} />
      </form>
      {#if !c.builtin}
        <button class="mini" type="button" aria-label={t('commandRow.deleteAria', { name: c.name })} onclick={onDelete}>
          <Icon name="trash" size={15} />
        </button>
      {:else}
        <span class="mini-spacer" aria-hidden="true"></span>
      {/if}
    {/snippet}
  </ManagementRow>
</div>

<style>
  /* Selection accent: a 2px green edge on the selected row, so the list still
     says which command the docked inspector is holding once the row scrolls
     away from the inspector's own header. Applied from here rather than in the
     shared ManagementRow: only the command deck docks an inspector beside its
     list, and the other decks select without one. */
  .row-wrap :global(.mrow) { position: relative; }
  .row-wrap :global(.mrow.selected)::before {
    content: '';
    position: absolute;
    left: 0;
    top: 0;
    bottom: 0;
    width: 2px;
    background: var(--bb-green-glow, #52b788);
  }

  .prow {
    display: grid;
    /* One fixed track per cell rather than a nested metadata grid: the deck
       reads as columns, so permission / uses / cooldown / state must land on
       the same x on every row, including rows whose response is short. */
    grid-template-columns: 22px 190px minmax(0, 1fr) 104px 92px 44px auto;
    align-items: center;
    gap: 14px;
    /* Pin every row to one height: the tallest cell (perm pill vs uses stack)
       differs by a rounding pixel between custom and built-in rows otherwise. */
    min-height: 26px;
  }

  .idx { font-family: var(--bb-font-mono); font-size: 10px; color: var(--bb-muted); opacity: 0.55; }

  /* Single-line cell: aliases ride inline after the name so every row is the
     same height — a second stacked line made alias rows taller than the rest. */
  .cmd { display: flex; align-items: center; gap: 8px; min-width: 0; overflow: hidden; }
  .cmd-name {
    display: inline-flex; align-items: center; gap: 2px;
    font-family: var(--bb-font-mono); font-size: 13.5px; color: var(--bb-tan-light);
    /* Never wraps: a name + badge combo wider than the 190px name column used
       to fold to a second line and make that row taller than its neighbors. */
    white-space: nowrap;
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

  .aliases { display: flex; flex-wrap: nowrap; gap: 4px; min-width: 0; overflow: hidden; }
  .alias-tag {
    font-family: var(--bb-font-mono);
    font-size: 10px;
    color: var(--bb-muted);
    border: 1px solid var(--rule, rgba(240, 236, 228, 0.1));
    border-radius: 8px;
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

  .m-perm { display: inline-flex; align-items: center; min-width: 0; justify-self: start; }

  .m-uses { display: flex; flex-direction: column; gap: 5px; min-width: 0; }
  .u-line { display: flex; align-items: baseline; gap: 6px; }
  .u-track { display: block; height: 2px; background: rgba(240, 236, 228, 0.09); }
  .u-fill {
    display: block;
    height: 2px;
    background: var(--bb-green-glow, #52b788);
    transition: width var(--bb-dur-base, 320ms) var(--bb-ease-out-expo, ease);
  }
  /* A disabled command keeps its history but stops being a live signal. */
  :global(.mrow.off) .u-fill { background: var(--bb-muted); }

  .m-cd {
    font-family: var(--bb-font-mono);
    font-size: 11.5px;
    color: var(--bb-muted);
    text-align: right;
    font-variant-numeric: tabular-nums;
  }

  .m-lbl {
    font-family: var(--bb-font-body);
    font-size: 10px;
    letter-spacing: 0.04em;
    text-transform: uppercase;
    color: var(--bb-muted);
    opacity: 0.7;
    white-space: nowrap;
  }
  .m-val {
    font-family: var(--bb-font-mono);
    font-size: 11.5px;
    color: var(--bb-muted);
    white-space: nowrap;
    font-variant-numeric: tabular-nums;
  }
  .m-val.uses { color: var(--bb-white); }
  .state { min-width: 0; }

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
  .mini-spacer { width: 32px; height: 32px; flex: none; }

  /* Mid width: the index and the cooldown are the first things to go — the
     row still ranks by use, and the cooldown lives in the inspector. */
  @media (max-width: 1080px) {
    .prow { grid-template-columns: 190px minmax(0, 1fr) 104px 92px auto; }
    .idx, .m-cd { display: none; }
  }

  @media (max-width: 760px) {
    .prow {
      grid-template-columns: minmax(0, 1fr);
      grid-template-areas:
        'cmd'
        'resp'
        'perm'
        'uses';
      row-gap: 6px;
    }
    .cmd { grid-area: cmd; flex-wrap: wrap; }
    .aliases { flex-wrap: wrap; overflow: visible; }
    .resp { grid-area: resp; white-space: normal; display: -webkit-box; -webkit-line-clamp: 2; -webkit-box-orient: vertical; }
    .m-perm { grid-area: perm; }
    .m-uses { grid-area: uses; width: 130px; }
    .state { display: none; }
    .mini, .mini-spacer { min-width: 44px; min-height: 44px; }
  }
</style>
