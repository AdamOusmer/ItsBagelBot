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
  import { stateOf, tierOf, USER_BADGE_LABEL } from './user-state';

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
  const dates = userDates(locale);
  const tier = $derived(tierOf(user));
  const state = $derived(stateOf(user));
  const meta = $derived(t('admin.users.rowMeta', { id: String(user.id), joined: dates.ago(user.created_at) }));
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
      {@render stateBadge(tier)}
      {#if state !== tier}
        {@render stateBadge(state)}
      {/if}
      {#if user.creator_code}
        <StatePill tone="neutral">{t('admin.users.rowCode')}</StatePill>
      {/if}
    </span>
  {/snippet}
  {#snippet actions()}
    <ChannelLink login={user.username} label={t('admin.users.channelLink', { login: user.username })} />
  {/snippet}
</ManagementRow>

{#snippet stateBadge(value: typeof state)}
  <StatePill tone={value}>
    <span class="badge-label"><bb-fit><span data-fit>{t(USER_BADGE_LABEL[value])}</span></bb-fit></span>
  </StatePill>
{/snippet}

<style>
  .marks {
    display: contents;
  }

  .badge-label {
    display: block;
    inline-size: 6.25rem;
    text-align: center;
  }

  @media (max-width: 560px) {
    .marks {
      display: none;
    }
  }
</style>
