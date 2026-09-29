<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import ManagementRow from '@bagel/ui/svelte/ManagementRow.svelte';
  import Bolota from '@bagel/kit/components/Bolota.svelte';
  import { ago } from '@bagel/kit';
  import { getI18n } from '@bagel/kit/i18n/context';
  import type { AdminAcct } from '$lib/server/services';
  import Tag from '@bagel/ui/svelte/Tag.svelte';
  import StatePill from '../StatePill.svelte';
  import { ROLE_LABEL } from './staff-roles';

  let {
    member,
    isSelf,
    selected,
    controls,
    onselect
  }: {
    member: AdminAcct;
    isSelf: boolean;
    selected: boolean;
    controls: string;
    onselect: () => void;
  } = $props();

  const { t } = getI18n();
</script>

{#snippet you()}
  <Tag tone="quiet">{t('admin.staff.you')}</Tag>
{/snippet}

<ManagementRow
  wrap
  {selected}
  expanded={selected}
  {controls}
  {onselect}
  badge={isSelf ? you : undefined}
  title={member.display_name || member.login}
  meta={t('admin.staff.rowMeta', {
    login: member.login,
    id: String(member.id),
    when: ago(member.created_at)
  })}
>
  {#snippet lead()}
    <Bolota name={member.display_name || member.login} size={28} active={selected} />
  {/snippet}
  {#snippet marks()}
    <StatePill tone={member.role}>{t(ROLE_LABEL[member.role])}</StatePill>
    <span class="access">
      <StatePill tone={member.active ? 'free' : 'inactive'}>
        {member.active ? t('admin.staff.activeChip') : t('admin.staff.inactiveChip')}
      </StatePill>
    </span>
  {/snippet}
</ManagementRow>

<style>
  .access {
    display: contents;
  }
  @media (max-width: 560px) {
    .access {
      display: none;
    }
  }
</style>
