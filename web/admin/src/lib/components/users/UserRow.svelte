<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  // One directory row on the shared ManagementRow. It carries no `actions`
  // snippet on purpose: every mutation this user has is destructive or
  // role-gated, so they all live in the inspector where the confirmation and
  // the permission story sit together, and the row stays a pure selector.
  //
  // ManagementRow already sets data-cursor="off" on its primary button (a row
  // is a reading surface, not a control, so the custom cursor must not morph
  // into a row-sized box).
  import ManagementRow from '@bagel/shared/components/ManagementRow.svelte';
  import Bolota from '@bagel/shared/components/Bolota.svelte';
  import { ago } from '@bagel/shared';
  import { getI18n } from '@bagel/shared/i18n/context';
  import type { AdminUserWire } from '$lib/server/services';
  import StatePill from '../StatePill.svelte';
  import { stateOf } from './user-state';

  let {
    user,
    selected,
    controls,
    onselect
  }: {
    user: AdminUserWire;
    selected: boolean;
    controls: string;
    onselect: () => void;
  } = $props();

  const { t } = getI18n();

  const state = $derived(stateOf(user));
  // `gate` because this list can run to fifteen rows: the blob engine only
  // mounts for avatars actually on screen.
</script>

<ManagementRow {selected} expanded={selected} {controls} {onselect}>
  {#snippet primary()}
    <span class="row">
      <Bolota name={user.username} size={28} gate active={selected} />
      <span class="who">
        <span class="login">{user.username}</span>
        <span class="meta">
          {t('admin.users.rowMeta', { id: String(user.id), joined: ago(user.created_at) })}
        </span>
      </span>
      <span class="marks">
        <StatePill tone={user.status as 'free' | 'paid' | 'vip'}>{user.status}</StatePill>
        {#if state === 'banned' || state === 'inactive'}
          <StatePill tone={state}>{state}</StatePill>
        {/if}
        {#if user.creator_code}
          <StatePill tone="neutral">{t('admin.users.rowCode')}</StatePill>
        {/if}
      </span>
    </span>
  {/snippet}
</ManagementRow>

<style>
  .row {
    display: flex;
    align-items: center;
    gap: 12px;
    min-width: 0;
  }
  .who {
    display: flex;
    flex-direction: column;
    gap: 2px;
    min-width: 0;
    flex: 1;
  }
  .login {
    font-family: var(--bb-font-body);
    font-weight: 600;
    font-size: 13.5px;
    color: var(--bb-white);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .meta {
    font-family: var(--bb-font-mono);
    font-size: 11px;
    color: var(--bb-muted);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .marks {
    display: flex;
    gap: 6px;
    flex-wrap: wrap;
    justify-content: flex-end;
    flex: none;
  }
  @media (max-width: 560px) {
    .marks {
      display: none;
    }
  }
</style>
