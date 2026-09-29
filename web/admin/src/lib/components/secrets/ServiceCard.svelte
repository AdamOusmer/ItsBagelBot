<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import Card from '@bagel/ui/svelte/Card.svelte';
  import CardHead from '@bagel/ui/svelte/CardHead.svelte';
  import Button from '@bagel/ui/svelte/Button.svelte';
  import FactList from '@bagel/ui/svelte/FactList.svelte';
  import Fact from '@bagel/ui/svelte/Fact.svelte';
  import { getI18n } from '@bagel/kit/i18n/context';
  import type { DbCredentialStatus } from '$lib/server/secrets';
  import StatePill from '../StatePill.svelte';

  let {
    service,
    canManage,
    onRotate,
    onSet,
    onRevoke
  }: {
    service: DbCredentialStatus;
    canManage: boolean;
    onRotate: () => void;
    onSet: () => void;
    onRevoke: () => void;
  } = $props();

  const { t } = getI18n();

  const scoped = $derived(service.tokenSource === 'scoped');

  const dbUserMissing = $derived(service.canReadDoppler && !service.dbUser);

  const dbUserLabel = $derived(
    !service.canReadDoppler
      ? t('admin.secrets.userUnreadable')
      : service.dbUser || t('admin.secrets.userUnset')
  );
</script>

<Card>
  <CardHead title={service.label}>
    {#snippet action()}
      <StatePill tone={scoped ? 'free' : 'banned'}>
        {scoped ? t('admin.secrets.tokenScoped') : t('admin.secrets.tokenMissing')}
      </StatePill>
    {/snippet}
  </CardHead>

  <div class="facts">
    <FactList>
      <Fact term={t('admin.secrets.factDoppler')} truncate>{service.project}/{service.config}</Fact>
      <Fact term={t('admin.secrets.factSchema')} truncate>{service.schema}</Fact>
      <Fact term={t('admin.secrets.factDbUser')} tone={dbUserMissing ? 'danger' : undefined} truncate>
        {dbUserLabel}
      </Fact>
      <Fact term={t('admin.secrets.factAutoMigrate')} title={t('admin.secrets.autoMigrateNote')} truncate>
        {service.autoMigrate || '-'}
      </Fact>
    </FactList>
  </div>

  {#if canManage}
    <div class="actions">
      <Button variant="ghost" onclick={onRotate}>{t('admin.secrets.rotate')}</Button>
      <Button variant="ghost" onclick={onSet}>{t('admin.secrets.set')}</Button>
      <Button onclick={onRevoke} tone="danger">{t('admin.secrets.revoke')}</Button>
    </div>
  {/if}
</Card>

<style>
  .facts {
    margin-bottom: 14px;
  }
  .actions {
    display: flex;
    gap: var(--bb-space-2);
    flex-wrap: wrap;
  }
</style>
