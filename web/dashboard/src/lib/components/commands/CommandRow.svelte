<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import { formatCounterValue } from '@bagel/kit/validation';
  import { enhance } from '$app/forms';
  import type { SubmitFunction } from '@sveltejs/kit';
  import {
    Icon,
    IconButton,
    PermBadge,
    ProgressBar,
    SaveStatus,
    ManagementRow,
    Switch,
    Tag,
    Text,
    getI18n,
    usesCount,
    type CommandView,
    type Perm
  } from '@bagel/kit';
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
  const cd = $derived(c.cooldown && c.cooldown > 0 ? `${c.cooldown}s` : '-');
  const idx = $derived(index !== undefined ? String(index).padStart(2, '0') : '');
  const uses = $derived(usesCount(c));
  const usesShare = $derived(usesMax > 0n ? Math.min(100, Number((uses * 100n + usesMax / 2n) / usesMax)) / 100 : 0);
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
        {#if idx}<span class="idx" aria-hidden="true"><Text as="span" size="xs" mono tone="muted">{idx}</Text></span>{/if}
        <span class="cmd">
          <span class="cmd-name">
            <Text as="span" size="sm" mono tone="accent">!{c.name}</Text>
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
              {#each c.aliases as a}<Tag bare literal>{a}</Tag>{/each}
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
          <ProgressBar
            value={usesShare}
            size="sm"
            tone={c.is_active ? 'success' : 'neutral'}
            label={t('commandRow.uses')}
            aria-hidden="true"
          />
        </span>
        <span class="m-cd"><Text as="span" size="xs" mono tone="muted">{cd}</Text></span>
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
        <IconButton size="sm" danger label={t('commandRow.deleteAria', { name: c.name })} onclick={onDelete}><Icon name="trash" size={15} /></IconButton>
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

  .idx { opacity: 0.55; }

  .cmd { display: flex; align-items: center; gap: 8px; min-width: 0; overflow: hidden; }
  .cmd-name { display: inline-flex; align-items: center; gap: 2px; white-space: nowrap; }
  .lock { display: inline-flex; color: var(--bb-muted); margin-left: 6px; vertical-align: middle; }

  .name-tag { margin-left: 8px; display: inline-flex; }

  .aliases { display: flex; flex-wrap: nowrap; gap: 12px; min-width: 0; overflow: hidden; }

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

  .row-wrap { --btn-icon-min-size: 32px; }
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
    .resp { grid-area: resp; -webkit-line-clamp: 2; line-clamp: 2; }
    .m-perm { grid-area: perm; }
    .m-uses { grid-area: uses; width: 130px; }
    .state { display: none; }
    .row-wrap { --btn-icon-min-size: 44px; }
    .mini-spacer { min-width: 44px; min-height: 44px; }
  }
</style>
