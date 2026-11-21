<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import Card from '@bagel/ui/svelte/Card.svelte';
  import CardHead from '@bagel/ui/svelte/CardHead.svelte';
  import Button from '@bagel/ui/svelte/Button.svelte';
  import FactList from '@bagel/ui/svelte/FactList.svelte';
  import Fact from '@bagel/ui/svelte/Fact.svelte';
  import Tag from '@bagel/ui/svelte/Tag.svelte';
  import { getI18n } from '@bagel/kit/i18n/context';
  import type { DbCredentialStatus } from '#lib/server/secrets';
  import { SECRET_DIALOGS, type SecretVerb } from './secret-dialogs';

  let { service, canManage, refreshing, onAction }: {
    service: DbCredentialStatus;
    canManage: boolean;
    refreshing: boolean;
    onAction: (verb: SecretVerb) => void;
  } = $props();

  const { t } = getI18n();
  const actions = ['rotate', 'set', 'revoke'] as const;

  const tokenMissing = $derived(service.tokenSource === 'missing');
  const status = $derived(tokenMissing ? 'missing' : service.canReadDoppler ? 'scoped' : 'unreadable');
  const statuses = {
    scoped: { text: 'admin.secrets.tokenScoped', tone: 'free' },
    missing: { text: 'admin.secrets.tokenMissing', tone: 'banned' },
    unreadable: { text: 'admin.secrets.tokenUnreadable', tone: 'inactive' }
  } as const;

  const dbUserMissing = $derived(service.canReadDoppler && !service.dbUser);
  const dbUserLabel = $derived(
    service.canReadDoppler ? service.dbUser || t('admin.secrets.userUnset') : t('admin.secrets.userUnreadable')
  );
</script>

<Card as="article" style="--card-pad: var(--bb-space-4); block-size: 100%;">
  <div class="service-content">
    <CardHead title={service.label} headingLevel={2}>
      {#snippet actions()}
        <Tag tone={statuses[status].tone} bare literal style="inline-size: min(45%, 9rem); justify-content: end;">
          <bb-fit><span data-fit>{t(statuses[status].text)}</span></bb-fit>
        </Tag>
      {/snippet}
    </CardHead>

    <FactList>
      <Fact term={t('admin.secrets.factDoppler')} title={service.project + '/' + service.config} class="credential-fact" truncate fit>
        {service.project}/{service.config}
      </Fact>
      <Fact term={t('admin.secrets.factSchema')} title={service.schema} class="credential-fact" truncate fit>{service.schema || '–'}</Fact>
      <Fact term={t('admin.secrets.factDbUser')} title={dbUserLabel} tone={dbUserMissing ? 'danger' : undefined} class="credential-fact" truncate fit>
        {dbUserLabel}
      </Fact>
      <Fact term={t('admin.secrets.factAutoMigrate')} title={t('admin.secrets.autoMigrateNote')} class="credential-fact" truncate fit>
        {service.autoMigrate || '–'}
      </Fact>
    </FactList>

    <div class="credential-actions" class:visible={canManage} class:refreshing inert={!canManage}>
      {#each actions as verb (verb)}
        <Button
          block
          variant="ghost"
          tone={SECRET_DIALOGS[verb].danger ? 'danger' : 'neutral'}
          aria-label={t('admin.secrets.credentialAction', { action: t(SECRET_DIALOGS[verb].label), service: service.label })}
          aria-disabled={refreshing}
          onclick={() => { if (!refreshing) onAction(verb); }}
        >
          <bb-fit><span data-fit>{t(SECRET_DIALOGS[verb].label)}</span></bb-fit>
        </Button>
      {/each}
    </div>
  </div>
</Card>

<style>
  .service-content {
    display: flex;
    flex-direction: column;
    block-size: 100%;
    min-inline-size: 0;
  }

  .service-content :global(.credential-fact) {
    display: grid;
    grid-template-columns: minmax(0, 2fr) minmax(0, 3fr);
  }

  .credential-actions {
    --btn-pad: 0 var(--bb-space-1);
    display: grid;
    grid-template-columns: repeat(3, minmax(0, 1fr));
    gap: var(--bb-space-1);
    margin-block-start: auto;
    opacity: 0;
    transition: opacity var(--bb-dur-fast) ease;
  }

  .credential-actions.visible {
    opacity: 1;
  }

  .credential-actions.visible.refreshing {
    opacity: 0.6;
  }
</style>
