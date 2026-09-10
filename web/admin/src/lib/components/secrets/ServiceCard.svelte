<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  // One service's runtime database credential: where it lives, who it connects
  // as, and the three verbs that change it.
  //
  // The buttons are siblings of the facts, not inside a clickable card: there is
  // no "open this service" -- every verb needs its own typed confirmation, so
  // there is nothing an inspector would add but a second click.
  //
  // `legacy` is gone from the token-source vocabulary: TokenSource is
  // 'scoped' | 'missing' since the broad token was pruned, and the card used to
  // carry a third branch that could never render.
  import Card from '@bagel/kit/components/Card.svelte';
  import CardHead from '@bagel/kit/components/CardHead.svelte';
  import Button from '@bagel/kit/components/Button.svelte';
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

  // Three states, not two: an unreadable value is not the same as an absent
  // one, and rendering "not set" for a token this console cannot read would
  // invite an operator to provision a user that already exists.
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
      <!-- Doppler's value only. deploy/k8s/*.yaml pins DB_AUTO_MIGRATE as a pod
           env var, which outranks this, so production may differ. -->
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
