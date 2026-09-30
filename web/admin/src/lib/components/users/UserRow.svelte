<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import ManagementRow from '@bagel/ui/svelte/ManagementRow.svelte';
  import Bolota from '@bagel/kit/components/Bolota.svelte';
  import { ago } from '@bagel/kit';
  import { getI18n } from '@bagel/kit/i18n/context';
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
</script>

<ManagementRow
  {selected}
  expanded={selected}
  {controls}
  onSelect={onselect}
  title={user.username}
  meta={t('admin.users.rowMeta', { id: String(user.id), joined: ago(user.created_at) })}
>
  {#snippet leading()}
    <Bolota name={user.username} size={28} gate active={selected} />
  {/snippet}
  {#snippet marks()}
    <span class="marks">
      <StatePill tone={user.status as 'free' | 'paid' | 'vip'}>{user.status}</StatePill>
      {#if state === 'banned' || state === 'inactive'}
        <StatePill tone={state}>{state}</StatePill>
      {/if}
      {#if user.creator_code}
        <StatePill tone="neutral">{t('admin.users.rowCode')}</StatePill>
      {/if}
    </span>
  {/snippet}
</ManagementRow>

<style>
  .marks {
    display: contents;
  }
  @media (max-width: 560px) {
    .marks {
      display: none;
    }
  }
</style>
