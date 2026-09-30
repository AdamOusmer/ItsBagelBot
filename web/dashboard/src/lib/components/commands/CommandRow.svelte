<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import { formatCounterValue } from '@bagel/kit/validation';
  import { enhance } from '$app/forms';
  import type { SubmitFunction } from '@sveltejs/kit';
  import Icon from '@bagel/ui/svelte/Icon.svelte';
  import ProgressBar from '@bagel/ui/svelte/ProgressBar.svelte';
  import SaveStatus from '@bagel/ui/svelte/SaveStatus.svelte';
  import ManagementRow from '@bagel/ui/svelte/ManagementRow.svelte';
  import Switch from '@bagel/ui/svelte/Switch.svelte';
  import Tag from '@bagel/ui/svelte/Tag.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import {
    PermBadge,
    getI18n,
    usesCount,
    type CommandView,
    type Perm
  } from '@bagel/kit';
  import VisuallyHidden from '@bagel/ui/svelte/VisuallyHidden.svelte';
  import RowDeleteButton from '$lib/components/shared/RowDeleteButton.svelte';
  import type { SaveState } from '@bagel/ui/svelte/SaveStatus.svelte';

  const { t } = getI18n();
  const ALIAS_VISIBLE = 2;

  let {
    command,
    index = undefined as number | undefined,
    usesMax = 0n,
    status = 'idle' as SaveState,
    unsaved = false,
    expanded = false,
    selecting = false,
    checked = false,
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
    selecting?: boolean;
    checked?: boolean;
    onExpand: () => void;
    onDelete: () => void;
    toggleSubmit: SubmitFunction;
  } = $props();

  const c = $derived(command);
  const cd = $derived(c.cooldown && c.cooldown > 0 ? `${c.cooldown}s` : '-');
  const idx = $derived(index !== undefined ? String(index).padStart(2, '0') : '');
  const uses = $derived(usesCount(c));
  const aliasList = $derived(c.aliases ?? []);
  const aliasAll = $derived(aliasList.join(', '));
  const saving = $derived(status === 'saving');
  const statusLabels = $derived({
    savingLabel: t('commandEditor.saving'),
    savedLabel: t('commands.saved'),
    errorLabel: t('commandRow.failed')
  });
  const statusLabel = $derived(
    status === 'saving' ? statusLabels.savingLabel : status === 'saved' ? statusLabels.savedLabel : status === 'error' ? statusLabels.errorLabel : undefined
  );
  const usesShare = $derived(usesMax > 0n ? Math.min(100, Number((uses * 100n + usesMax / 2n) / usesMax)) / 100 : 0);
</script>

<div class="row-wrap" class:flash-save={status === 'saved'} data-row-name={c.name}>
  <ManagementRow
    accent
    selected={expanded}
    {expanded}
    disabled={!c.is_active}
    onSelect={onExpand}
  >
    {#snippet primary()}
      <span class="prow" class:selecting>
        <span class="idx">
          {#if selecting}
            <span class="pick" class:on={checked} aria-hidden="true">{#if checked}<Icon name="check" size={11} />{/if}</span>
            <VisuallyHidden>{checked ? t('commandRow.selected') : t('commandRow.notSelected')}</VisuallyHidden>
          {:else}<Text as="span" size="xs" mono tone="muted" aria-hidden="true">{idx}</Text>{/if}
        </span>
        <span class="cmd">
          <span class="cmd-name">
            <Text as="span" size="sm" mono tone="accent">!{c.name}</Text>
            {#if c.allowed_user_id}
              <span class="lock" role="img" aria-label={t('commandRow.lockedTo', { id: c.allowed_user_id })} title={t('commandRow.lockedTo', { id: c.allowed_user_id })}><Icon name="lock" size={11} /></span>
            {/if}
            {#if c.stream_online_only}
              <span class="lock" role="img" aria-label={t('commandRow.liveOnly')} title={t('commandRow.liveOnly')}><Icon name="pulse" size={11} /></span>
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
          {#if aliasList.length}
            <span class="aliases" title={t('commandRow.also', { aliases: aliasAll })}>
              {#each aliasList as a, i (a)}<span class="alias" class:extra={i >= ALIAS_VISIBLE}><Tag bare literal>{a}</Tag></span>{/each}
              {#if aliasList.length > ALIAS_VISIBLE}
                <span class="alias more" role="img" aria-label={t('commandRow.moreAliases', { count: aliasList.length - ALIAS_VISIBLE, aliases: aliasList.slice(ALIAS_VISIBLE).join(', ') })}><Tag bare literal>+{aliasList.length - ALIAS_VISIBLE}</Tag></span>
              {/if}
            </span>
          {/if}
        </span>
        <span class="resp"><Text as="span" size="sm" tone="muted">{c.response}</Text></span>
        <span class="m-perm"><PermBadge perm={(c.perm ?? 'everyone') as Perm} /></span>
        <span class="m-uses">
          <span class="u-line">
            <Text as="span" size="xs" mono>{formatCounterValue(uses.toString())}</Text>
            <span class="m-lbl">{t('commandRow.uses')}</span>
          </span>
          <span class="u-track">
            <ProgressBar
              value={usesShare}
              size="sm"
              tone={c.is_active ? 'success' : 'neutral'}
              label={t('commandRow.uses')}
              aria-hidden="true"
            />
          </span>
        </span>
        <span class="m-cd"><Text as="span" size="xs" mono tone="muted">{cd}</Text></span>
        <span class="state"><SaveStatus state={status} {...statusLabels} /></span>
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
        <Switch type="submit" checked={c.is_active} busy={saving} label={t('commandRow.toggleAria', { name: c.name })} />
      </form>
      <span class="state-compact"><SaveStatus state={status} compact aria-label={statusLabel} {...statusLabels} /></span>
      {#if !c.builtin}
        <RowDeleteButton label={t('commandRow.deleteAria', { name: c.name })} onclick={onDelete} />
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

  .idx {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    min-height: 18px;
  }
  .idx:not(:has(.pick)) { opacity: 0.55; }

  .pick {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 18px;
    height: 18px;
    border: 1px solid var(--bb-muted);
    border-radius: var(--bb-radius-xs);
    color: var(--bb-black);
  }
  .pick.on { background: var(--bb-green-glow); border-color: var(--bb-green-glow); }

  .cmd { display: flex; align-items: center; gap: 8px; min-width: 0; overflow: hidden; }
  .cmd-name { display: inline-flex; align-items: center; gap: 2px; white-space: nowrap; }
  .lock { display: inline-flex; color: var(--bb-muted); margin-left: 6px; vertical-align: middle; }

  .name-tag { margin-left: 8px; display: inline-flex; }

  .aliases { display: flex; flex-wrap: nowrap; gap: 12px; min-width: 0; overflow: hidden; }
  .alias { display: inline-flex; }
  .aliases .extra { display: none; }
  .aliases .more { flex: none; }

  .resp {
    display: -webkit-box;
    -webkit-box-orient: vertical;
    -webkit-line-clamp: 1;
    line-clamp: 1;
    overflow: hidden;
    min-width: 0;
  }

  .m-perm { display: inline-flex; align-items: center; min-width: 0; justify-self: start; }

  .m-uses { display: flex; flex-direction: column; gap: 5px; min-width: 0; }
  .u-line { display: flex; align-items: baseline; gap: 6px; white-space: nowrap; font-variant-numeric: tabular-nums; }

  .u-track { display: block; }

  .m-cd { text-align: right; font-variant-numeric: tabular-nums; }

  .m-lbl {
    font-family: var(--bb-font-body);
    font-size: 10px;
    letter-spacing: 0.04em;
    text-transform: uppercase;
    color: var(--bb-muted);
    opacity: 0.7;
    white-space: nowrap;
  }
  .state { min-width: 0; }
  .state-compact { display: none; min-width: 22px; justify-content: center; }

  .mini-spacer { width: 32px; height: 32px; flex: none; }

  @container (max-width: 820px) {
    .prow { grid-template-columns: 190px minmax(0, 1fr) 104px 92px auto; }
    .prow.selecting { grid-template-columns: 22px 190px minmax(0, 1fr) 104px 92px auto; }
    .idx, .m-cd { display: none; }
    .selecting .idx { display: inline-flex; }
  }

  @container (max-width: 560px) {
    .prow {
      grid-template-columns: minmax(0, 1fr);
      grid-template-areas:
        'cmd'
        'resp'
        'perm'
        'uses';
      row-gap: 6px;
    }
    .prow.selecting {
      grid-template-columns: 22px minmax(0, 1fr);
      grid-template-areas:
        'idx cmd'
        '. resp'
        '. perm'
        '. uses';
    }
    .idx { grid-area: idx; }
    .cmd { grid-area: cmd; flex-wrap: wrap; }
    .aliases { flex-wrap: wrap; overflow: visible; }
    .aliases .extra { display: inline-flex; }
    .aliases .more { display: none; }
    .resp { grid-area: resp; -webkit-line-clamp: 2; line-clamp: 2; }
    .m-perm { grid-area: perm; }
    .m-uses { grid-area: uses; width: 130px; }
    .u-track { display: none; }
    .state { display: none; }
    .state-compact { display: inline-flex; }
  }

  @media (max-width: 760px), (pointer: coarse) {
    .mini-spacer { width: 44px; height: 44px; }
  }
</style>
