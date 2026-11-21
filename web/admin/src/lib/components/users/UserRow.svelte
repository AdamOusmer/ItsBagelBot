<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import ManagementRow from '@bagel/ui/svelte/ManagementRow.svelte';
  import Bolota from '@bagel/ui/svelte/Bolota.svelte';
  import { getI18n } from '@bagel/kit/i18n/context';
  import type { AdminUserWire } from '#lib/server/services';
  import StatePill from '../StatePill.svelte';
  import ChannelLink from './ChannelLink.svelte';
  import { userDates } from './user-dates';
  import { stateOf, tierOf, USER_BADGE_LABEL, type UserState } from './user-state';

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

  const { locale, t } = getI18n();
  const { ago } = userDates(locale);
  const tier = $derived(tierOf(user));
  const state = $derived(stateOf(user));
  const meta = $derived(t('admin.users.rowMeta', { id: String(user.id), joined: ago(user.created_at) }));
</script>

<ManagementRow
  {selected}
  expanded={selected}
  {controls}
  onSelect={onselect}
  title={user.username}
  {meta}
>
  {#snippet leading()}
    <Bolota name={user.username} size={28} gate active={selected} />
  {/snippet}
  {#snippet marks()}
    <span class="marks">
      {#if tier !== 'free'}{@render stateBadge(tier)}{/if}
      {#if state !== tier}{@render stateBadge(state)}{/if}
      {#if user.creator_code}{@render badge('neutral', t('admin.users.rowCode'))}{/if}
    </span>
  {/snippet}
  {#snippet actions()}
    <ChannelLink login={user.username} label={t('admin.users.channelLink', { login: user.username })} />
  {/snippet}
</ManagementRow>

{#snippet stateBadge(value: UserState)}
  {@render badge(value, t(USER_BADGE_LABEL[value]))}
{/snippet}

{#snippet badge(tone: UserState | 'neutral', label: string)}
  <StatePill {tone}>
    <span class="badge-label" class:code={tone === 'neutral'}><bb-fit><span data-fit>{label}</span></bb-fit></span>
  </StatePill>
{/snippet}

<style>
  .marks {
    display: inline-flex;
    justify-content: flex-end;
    align-items: center;
    gap: var(--bb-space-2);
  }

  .badge-label {
    display: block;
    inline-size: 4.75rem;
    text-align: center;
  }

  .code {
    inline-size: 2.5rem;
  }

  @media (max-width: 560px) {
    .marks {
      display: none;
    }
  }
</style>
