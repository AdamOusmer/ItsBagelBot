<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import Card from '@bagel/ui/svelte/Card.svelte';
  import CardHead from '@bagel/ui/svelte/CardHead.svelte';
  import Button from '@bagel/ui/svelte/Button.svelte';
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

  <dl class="facts">
    <div>
      <dt>{t('admin.secrets.factDoppler')}</dt>
      <dd>{service.project}/{service.config}</dd>
    </div>
    <div>
      <dt>{t('admin.secrets.factSchema')}</dt>
      <dd>{service.schema}</dd>
    </div>
    <div>
      <dt>{t('admin.secrets.factDbUser')}</dt>
      <dd class:missing={service.canReadDoppler && !service.dbUser}>{dbUserLabel}</dd>
    </div>
    <div>
      <dt>{t('admin.secrets.factAutoMigrate')}</dt>
      <dd title={t('admin.secrets.autoMigrateNote')}>{service.autoMigrate || '-'}</dd>
    </div>
  </dl>

  {#if canManage}
    <div class="actions">
      <Button variant="ghost" onclick={onRotate}>{t('admin.secrets.rotate')}</Button>
      <Button variant="ghost" onclick={onSet}>{t('admin.secrets.set')}</Button>
      <Button variant="destructive" onclick={onRevoke}>{t('admin.secrets.revoke')}</Button>
    </div>
  {/if}
</Card>

<style>
  .facts {
    display: flex;
    flex-direction: column;
    gap: 8px;
    margin: 0 0 14px;
  }
  .facts div {
    display: flex;
    justify-content: space-between;
    gap: 12px;
    align-items: baseline;
  }
  .facts dt {
    font-family: var(--bb-font-body);
    font-size: 12px;
    color: var(--bb-muted);
    flex: none;
  }
  .facts dd {
    margin: 0;
    font-family: var(--bb-font-mono);
    font-size: 12px;
    color: var(--bb-tan-light);
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .facts dd.missing {
    color: var(--bb-status-error);
  }

  .actions {
    display: flex;
    gap: 8px;
    flex-wrap: wrap;
  }
</style>
