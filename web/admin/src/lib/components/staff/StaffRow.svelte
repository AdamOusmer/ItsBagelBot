<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  import ManagementRow from '@bagel/ui/svelte/ManagementRow.svelte';
  import Bolota from '@bagel/ui/svelte/Bolota.svelte';
  import { getI18n } from '@bagel/kit/i18n/context';
  import type { AdminAcct } from '#lib/server/services';
  import Tag from '@bagel/ui/svelte/Tag.svelte';
  import StatePill from '../StatePill.svelte';
  import { ROLE_LABEL } from './staff-roles';

  let {
    member,
    isSelf,
    selected,
    expanded,
    onselect
  }: {
    member: AdminAcct;
    isSelf: boolean;
    selected: boolean;
    expanded: boolean;
    onselect: () => void;
  } = $props();

  const { t } = getI18n();
</script>

{#snippet you()}
  <Tag tone="quiet"><bb-fit><span data-fit>{t('admin.staff.you')}</span></bb-fit></Tag>
{/snippet}

<li class="member">
  <ManagementRow
    {selected}
    {expanded}
    controls={expanded ? 'staff-inspector' : undefined}
    onSelect={onselect}
    style="min-block-size: var(--staff-row-height)"
    badge={isSelf ? you : undefined}
    title={member.display_name || member.login}
    meta={t('admin.staff.rowMeta', { login: member.login, id: String(member.id) })}
  >
    {#snippet leading()}
      <Bolota name={member.display_name || member.login} size={28} active={selected} cycle={false} />
    {/snippet}
    {#snippet marks()}
      <span class="marks">
        <span class="mark bb-fit-box">
          <StatePill tone={member.role}><bb-fit><span data-fit>{t(ROLE_LABEL[member.role])}</span></bb-fit></StatePill>
        </span>
        <span class="mark bb-fit-box">
          <StatePill tone={member.active ? 'free' : 'inactive'}>
            <bb-fit><span data-fit>{t(member.active ? 'admin.staff.activeChip' : 'admin.staff.inactiveChip')}</span></bb-fit>
          </StatePill>
        </span>
      </span>
    {/snippet}
  </ManagementRow>
</li>

<style>
  .member {
    container-type: inline-size;
  }

  .marks {
    display: inline-grid;
    grid-template-columns: 7.5rem 5.25rem;
    gap: var(--bb-space-1);
    align-items: center;
  }

  .mark {
    display: inline-grid;
    justify-items: end;
    min-inline-size: 0;
  }

  @container (max-width: 28rem) {
    .marks {
      grid-template-columns: 6.75rem;
      gap: 2px;
    }
  }
</style>
