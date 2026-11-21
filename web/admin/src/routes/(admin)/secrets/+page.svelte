<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import { invalidate } from '$app/navigation';
  import { getI18n } from '@bagel/kit/i18n/context';
  import AlertBanner from '@bagel/ui/svelte/AlertBanner.svelte';
  import Button from '@bagel/ui/svelte/Button.svelte';
  import Chip from '@bagel/ui/svelte/Chip.svelte';
  import EmptyState from '@bagel/ui/svelte/EmptyState.svelte';
  import Grid from '@bagel/ui/svelte/Grid.svelte';
  import PageHead from '@bagel/ui/svelte/PageHead.svelte';
  import PageToolbar from '@bagel/ui/svelte/PageToolbar.svelte';
  import Skeleton from '@bagel/ui/svelte/Skeleton.svelte';
  import { allows } from '#lib/access';
  import CredentialDialog from '#lib/components/secrets/CredentialDialog.svelte';
  import SecretGenerator from '#lib/components/secrets/SecretGenerator.svelte';
  import ServiceCard from '#lib/components/secrets/ServiceCard.svelte';
  import type { SecretVerb } from '#lib/components/secrets/secret-dialogs';
  import type { DbCredentialStatus } from '#lib/server/secrets';
  import { applyStreamed } from '#lib/streamed-load';

  let { data } = $props();

  const { locale, t } = getI18n();
  const numberFormat = new Intl.NumberFormat(locale);
  const canManage = $derived(allows(data.role, 'secrets.manage'));

  const STATUS_LABELS = {
    all: 'admin.secrets.filterAll',
    readable: 'admin.secrets.filterReadable',
    unavailable: 'admin.secrets.filterUnavailable'
  } as const;
  type StatusFilter = keyof typeof STATUS_LABELS;
  const statusFilters = Object.keys(STATUS_LABELS) as StatusFilter[];

  let statuses = $state.raw<DbCredentialStatus[] | null>();
  let statusFilter = $state<StatusFilter>('all');
  let pending = $state.raw<{ verb: SecretVerb; service: DbCredentialStatus } | null>(null);
  let generatorOpen = $state(false);
  let refreshing = $state(false);

  $effect(() => {
    applyStreamed(data.statuses, (value) => {
      statuses = value;
      refreshing = false;
    });
  });

  const groups = $derived({
    all: statuses,
    readable: statuses?.filter((service) => service.canReadDoppler),
    unavailable: statuses?.filter((service) => !service.canReadDoppler)
  });
  const services = $derived(groups[statusFilter]);
  const counts = $derived({
    all: statuses?.length ?? data.serviceCount,
    readable: groups.readable?.length,
    unavailable: groups.unavailable?.length
  });

  async function refresh() {
    if (refreshing) return;
    refreshing = true;
    await invalidate('admin:secrets');
  }
</script>

<section class="screen active">
  <PageHead description={t('admin.secrets.description')}>
    {t('admin.secrets.titlePre')}<em>{t('admin.secrets.titleEm')}</em>
  </PageHead>

  <PageToolbar style="flex-wrap: wrap">
    {#snippet leading()}
      <div class="status-filters" role="group" aria-label={t('admin.secrets.filterLabel')}>
        {#each statusFilters as value (value)}
          {@const count = counts[value]}
          <Chip pressed={statusFilter === value} onclick={() => (statusFilter = value)}
            style="min-inline-size: 0; justify-content: space-between; padding-inline: var(--bb-space-2); gap: var(--bb-space-1)">
            <span class="filter-name"><bb-fit><span data-fit>{t(STATUS_LABELS[value])}</span></bb-fit></span>
            <span class="filter-count">{count === undefined ? '' : numberFormat.format(count)}</span>
          </Chip>
        {/each}
      </div>
    {/snippet}
    {#snippet trailing()}
      <div class="generator-action">
        <Button variant="ghost" onclick={() => (generatorOpen = true)} block>
          <bb-fit><span data-fit>{t('admin.secrets.genTitle')}</span></bb-fit>
        </Button>
      </div>
    {/snippet}
  </PageToolbar>

  <div class="service-inventory" aria-busy={statuses === undefined || refreshing}>
    {#if statuses === undefined}
      <Grid min="19rem">
        {#each Array(data.serviceCount) as _, i (i)}
          <div class="service-slot" aria-hidden="true">
            <Skeleton variant="block" height="100%" />
          </div>
        {/each}
      </Grid>
    {:else if statuses === null}
      <AlertBanner>
        {t('admin.secrets.statusFailed')}
        {#snippet actions()}
          <Button variant="ghost" size="sm" onclick={refresh} busy={refreshing}>{t('common.retry')}</Button>
        {/snippet}
      </AlertBanner>
    {:else if services?.length}
      <Grid min="19rem">
        {#each services as service (service.id)}
          <div class="service-slot">
            <ServiceCard {service} {canManage} {refreshing} onAction={(verb) => (pending = { verb, service })} />
          </div>
        {/each}
      </Grid>
    {:else}
      <EmptyState
        title={t('admin.secrets.empty')}
        body={statusFilter === 'all' ? undefined : t('admin.secrets.emptyFilter')}
        role="status"
      />
    {/if}
  </div>
</section>

{#if pending}
  <CredentialDialog
    service={pending.service}
    verb={pending.verb}
    onClose={() => (pending = null)}
    onSaved={refresh}
  />
{/if}

{#if generatorOpen}
  <SecretGenerator onClose={() => (generatorOpen = false)} />
{/if}

<style>
  .status-filters {
    display: grid;
    grid-template-columns: repeat(3, minmax(0, 1fr));
    gap: var(--bb-space-1);
    flex: 1 1 21rem;
    max-inline-size: 25rem;
    min-inline-size: 0;
  }

  .filter-name {
    flex: 1;
    min-inline-size: 0;
    text-align: start;
  }

  .filter-count {
    flex: 0 0 2ch;
    text-align: end;
    font-variant-numeric: tabular-nums;
  }

  .generator-action {
    flex: 0 0 13rem;
    margin-inline-start: auto;
  }

  .service-inventory {
    --credential-card-size: 14rem;
    min-block-size: var(--credential-card-size);
  }

  .service-slot {
    block-size: var(--credential-card-size);
    min-inline-size: 0;
  }

  @media (max-width: 600px) {
    .status-filters {
      flex-basis: 100%;
      max-inline-size: none;
    }
  }
</style>
