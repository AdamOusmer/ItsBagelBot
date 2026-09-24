<script lang="ts">
  import { formatCounterValue } from '@bagel/kit/validation';
  import { Button } from '@bagel/kit';
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import { enhance } from '$app/forms';
  import type { SubmitFunction } from '@sveltejs/kit';
  import { Icon, PermBadge, SaveStatus, ManagementRow, Switch, Tag, getI18n, usesCount, type CommandView, type Perm } from '@bagel/kit';
  import type { SaveState } from '@bagel/ui/svelte/SaveStatus.svelte';

  const { t } = getI18n();

  let {
    command,
    index = undefined as number | undefined,
    usesMax = 0n,
    status = 'idle' as SaveState,
    unsaved = false,
    expanded = false,
    onExpand,
    onDelete,
    toggleSubmit
  }: {
    command: CommandView;
    index?: number;
    usesMax?: bigint;
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
  const barPct = $derived(usesMax > 0n ? Math.min(100, Number((uses * 100n + usesMax / 2n) / usesMax)) : 0);
</script>

<div class="row-wrap" class:flash-save={status === 'saved'}>
  <ManagementRow
    accent
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
              <span class="name-tag">
                <Tag tone="bare" title={t('commandRow.builtinTitle')}>{t('commandRow.builtin')}</Tag>
              </span>
            {/if}
            {#if unsaved}
              <span class="name-tag">
                <Tag tone="alpha" title={t('commandRow.unsavedTitle')}>{t('commandRow.unsaved')}</Tag>
              </span>
            {/if}
          </span>
          {#if c.aliases?.length}
            <span class="aliases" title={t('commandRow.also', { aliases: c.aliases.join(', ') })}>
              {#each c.aliases as a}<span class="bb-tag bb-tag--bare bb-tag--literal">{a}</span>{/each}
            </span>
          {/if}
        </span>
        <span class="resp">{c.response}</span>
        <span class="m-perm"><PermBadge perm={(c.perm ?? 'everyone') as Perm} /></span>
        <span class="m-uses">
          <span class="u-line">
            <span class="m-val uses">{formatCounterValue(uses.toString())}</span>
            <span class="m-lbl">{t('commandRow.uses')}</span>
          </span>
          <span class="u-track" aria-hidden="true">
            <span class="u-fill" class:is-off={!c.is_active} style="width:{barPct}%"></span>
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
        <input type="hidden" name="bump_counter" value={c.bump_counter ?? ''} />
        <input type="hidden" name="stream_online_only" value={c.stream_online_only ? 'on' : ''} />
        <input type="hidden" name="is_active" value={c.is_active ? '' : 'on'} />
        <Switch type="submit" checked={c.is_active} label={t('commandRow.toggleAria', { name: c.name })} />
      </form>
      {#if !c.builtin}
        <Button variant="icon" size="sm" class="delete-action" danger type="button" aria-label={t('commandRow.deleteAria', { name: c.name })} onclick={onDelete} ><Icon name="trash" size={15} /></Button>
      {:else}
        <span class="mini-spacer" aria-hidden="true"></span>
      {/if}
    {/snippet}
  </ManagementRow>
</div>

<style>
  .prow {
    display: grid;
    grid-template-columns: 22px 190px minmax(0, 1fr) 104px 92px 44px auto;
    align-items: center;
    gap: 14px;
    min-height: 26px;
  }

  .idx { font-family: var(--bb-font-mono); font-size: 10px; color: var(--bb-muted); opacity: 0.55; }

  .cmd { display: flex; align-items: center; gap: 8px; min-width: 0; overflow: hidden; }
  .cmd-name {
    display: inline-flex; align-items: center; gap: 2px;
    font-family: var(--bb-font-mono); font-size: 13.5px; color: var(--bb-tan-light);
    white-space: nowrap;
  }
  .lock { display: inline-flex; color: var(--bb-muted); margin-left: 6px; vertical-align: middle; }

  .name-tag { margin-left: 8px; display: inline-flex; }

  .aliases { display: flex; flex-wrap: nowrap; gap: 12px; min-width: 0; overflow: hidden; }

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
  .u-fill.is-off { background: var(--bb-muted); }

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

  :global(.delete-action) { width: 32px; height: 32px; min-height: 32px; }
  .mini-spacer { width: 32px; height: 32px; flex: none; }

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
    .resp { grid-area: resp; white-space: normal; display: -webkit-box; -webkit-line-clamp: 2; line-clamp: 2; -webkit-box-orient: vertical; }
    .m-perm { grid-area: perm; }
    .m-uses { grid-area: uses; width: 130px; }
    .state { display: none; }
    :global(.delete-action), .mini-spacer { min-width: 44px; min-height: 44px; }
  }
</style>
