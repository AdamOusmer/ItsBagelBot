<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import Chip from '@bagel/ui/svelte/Chip.svelte';
  import Fact from '@bagel/ui/svelte/Fact.svelte';
  import FactList from '@bagel/ui/svelte/FactList.svelte';
  import Heading from '@bagel/ui/svelte/Heading.svelte';
  import Stack from '@bagel/ui/svelte/Stack.svelte';
  import { getI18n } from '@bagel/kit/i18n/context';
  import type { AdminUserWire } from '#lib/server/services';
  import { USER_TIERS, USER_BADGE_LABEL } from './user-state';

  let { user, disabled, onStatus, date }: {
    user: AdminUserWire;
    disabled: boolean;
    onStatus: (status: string) => void;
    date: (value?: string | null) => string;
  } = $props();

  const { t } = getI18n();
  const details = $derived([
    { label: t('admin.users.planUntil'), value: user.subscription_expires_at ? date(user.subscription_expires_at) : '' },
    { label: t('admin.users.planSource'), value: user.subscription_source },
    { label: t('admin.users.planReference'), value: user.subscription_ref },
    { label: t('admin.users.planRenewal'), value: user.subscription_cancel_pending ? t('admin.users.planCancelPending') : '', tone: 'danger' as const }
  ].filter((fact) => fact.value));
</script>

<Stack as="section" gap={2}>
  <Heading level={3} variant="label">{t('admin.users.tierTitle')}</Heading>
  <div class="tiers" role="group" aria-label={t('admin.users.tierTitle')}>
    {#each USER_TIERS as tier (tier)}
      <Chip tone={tier} pressed={user.status === tier} {disabled} onclick={() => onStatus(tier)}>
        <span class="tier-label"><bb-fit><span data-fit>{t(USER_BADGE_LABEL[tier])}</span></bb-fit></span>
      </Chip>
    {/each}
  </div>
  {#if details.length}
    <FactList>
      {#each details as fact (fact.label)}
        <Fact term={fact.label} tone={fact.tone} truncate title={fact.value}>{fact.value}</Fact>
      {/each}
    </FactList>
  {/if}
</Stack>

<style>
  .tiers {
    display: grid;
    grid-template-columns: repeat(3, minmax(0, 1fr));
    gap: var(--bb-space-2);
  }

  .tier-label {
    inline-size: 100%;
    min-inline-size: 0;
    text-align: center;
  }
</style>
