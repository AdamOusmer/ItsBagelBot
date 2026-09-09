<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  // One roster row on the shared ManagementRow. No `actions` snippet: the role
  // select and the delete button used to sit here, which meant a <select> and a
  // <button> lived beside a row the whole of which was clickable. Both moved
  // into the inspector, so the row is a selector and nothing else.
  import ManagementRow from '@bagel/shared/components/ManagementRow.svelte';
  import Bolota from '@bagel/shared/components/Bolota.svelte';
  import { ago } from '@bagel/shared';
  import { getI18n } from '@bagel/shared/i18n/context';
  import type { AdminAcct } from '$lib/server/services';
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

<ManagementRow {selected} expanded={selected} {controls} {onselect}>
  {#snippet primary()}
    <span class="row">
      <Bolota name={member.display_name || member.login} size={28} active={selected} />
      <span class="who">
        <span class="name">
          {member.display_name || member.login}
          {#if isSelf}<span class="you">{t('admin.staff.you')}</span>{/if}
        </span>
        <span class="meta">
          {t('admin.staff.rowMeta', {
            login: member.login,
            id: String(member.id),
            when: ago(member.created_at)
          })}
        </span>
      </span>
      <span class="marks">
        <StatePill tone={member.role}>{t(ROLE_LABEL[member.role])}</StatePill>
        <StatePill tone={member.active ? 'free' : 'inactive'}>
          {member.active ? t('admin.staff.activeChip') : t('admin.staff.inactiveChip')}
        </StatePill>
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
  .name {
    display: flex;
    align-items: center;
    gap: 8px;
    font-family: var(--bb-font-body);
    font-weight: 600;
    font-size: 13.5px;
    color: var(--bb-white);
  }
  .you {
    font-family: var(--bb-font-mono);
    font-size: 9.5px;
    letter-spacing: 0.1em;
    text-transform: uppercase;
    color: var(--bb-tan-light);
    border: 1px solid rgba(201, 168, 124, 0.3);
    border-radius: var(--bb-radius-pill);
    padding: 1px 7px;
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
    flex: none;
  }
  @media (max-width: 560px) {
    .marks :global(.pill:last-child) {
      display: none;
    }
  }
</style>
