<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import { enhance, type SubmitFunction } from '$app/forms';
  import AlertBanner from '@bagel/ui/svelte/AlertBanner.svelte';
  import Button from '@bagel/ui/svelte/Button.svelte';
  import Card from '@bagel/ui/svelte/Card.svelte';
  import Cluster from '@bagel/ui/svelte/Cluster.svelte';
  import EmptyState from '@bagel/ui/svelte/EmptyState.svelte';
  import Field from '@bagel/ui/svelte/Field.svelte';
  import Heading from '@bagel/ui/svelte/Heading.svelte';
  import Input from '@bagel/ui/svelte/Input.svelte';
  import Scroller from '@bagel/ui/svelte/Scroller.svelte';
  import Select from '@bagel/ui/svelte/Select.svelte';
  import Stack from '@bagel/ui/svelte/Stack.svelte';
  import StatusDot from '@bagel/ui/svelte/StatusDot.svelte';
  import Tag from '@bagel/ui/svelte/Tag.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import { toast } from '@bagel/ui/svelte/toast';
  import { actionPayload, adminToastFailure } from '@bagel/kit';
  import { getI18n } from '@bagel/kit/i18n/context';
  import type { FluxResult, FluxTier } from '#lib/deploys/types';

  let { flux }: { flux: FluxResult } = $props();

  const { t } = getI18n();
  const failed = adminToastFailure(toast);

  let updated = $state.raw<{ source: FluxResult; snapshot: FluxResult } | null>(null);
  let pending = $state<string[]>([]);
  let service = $state('');
  let tag = $state('');

  const snapshot = $derived(updated?.source === flux ? updated.snapshot : flux);
  const tiers = $derived(snapshot.tiers);
  const pins = $derived(tiers.flatMap((tier) => tier.images.map((image) => ({ tier: tier.name, ...image }))));
  const services = $derived(snapshot.pinnable.map((name) => ({ value: name, label: name })));

  const tierStates = {
    paused: 'warning',
    unknown: 'neutral',
    ready: 'success',
    notReady: 'danger'
  } as const;

  function tierState(tier: FluxTier): keyof typeof tierStates {
    if (tier.suspended) return 'paused';
    if (tier.ready === null) return 'unknown';
    return tier.ready ? 'ready' : 'notReady';
  }

  function shortRevision(revision: string): string {
    const sha = revision.split(':').pop() ?? '';
    return sha.slice(0, 12);
  }

  function shortService(image: string): string {
    return image.split('/').pop() ?? image;
  }

  function submit(key: string): SubmitFunction {
    return ({ cancel }) => {
      if (pending.includes(key)) {
        cancel();
        return;
      }
      const source = flux;
      pending = [...pending, key];
      return async ({ result }) => {
        pending = pending.filter((action) => action !== key);
        const payload = actionPayload<{ flux?: FluxResult; error?: string }>(result);
        if (payload?.flux) {
          updated = { source, snapshot: payload.flux };
          if (key === 'pin') tag = '';
          return;
        }
        failed(payload, t('admin.deploys.actionFailed'));
      };
    };
  }
</script>

<Scroller class="deploy-flux-scroll" role="region" aria-labelledby="flux-title" tabindex={0}>
  <Card as="section">
    <Stack gap={4}>
      <Stack gap={1}>
        <Heading level={3} as="h2" id="flux-title">{t('admin.deploys.flux.title')}</Heading>
        <Text size="sm" tone="muted">{t('admin.deploys.flux.body')}</Text>
      </Stack>

      {#if snapshot.fluxError}
        <AlertBanner tone="danger">{t('admin.deploys.flux.error', { error: snapshot.fluxError })}</AlertBanner>
      {:else if !tiers.length}
        <EmptyState title={t('admin.deploys.flux.noTiers')} />
      {/if}

      {#if tiers.length}
        <ul class="tiers">
          {#each tiers as tier (tier.name)}
            {@const state = tierState(tier)}
            {@const tierLabel = `flux-tier-${tier.name}`}
            <li class="tier">
              <div class="name">
                <StatusDot tone={tierStates[state]} />
                <Text as="span" mono id={tierLabel}>{tier.name}</Text>
                <Text as="span" size="xs" tone="muted" class="tier-state">
                  <bb-fit><span data-fit>{t(`admin.deploys.flux.${state}`)}</span></bb-fit>
                </Text>
              </div>
              <Text as="span" size="xs" mono tone="soft" truncate title={tier.ready === false ? tier.message : undefined}>
                {tier.ready === false ? tier.reason : shortRevision(tier.revision)}
              </Text>
              <Cluster gap={2} class="tier-actions">
                <form method="POST" action="?/fluxSync" use:enhance={submit(`sync-${tier.name}`)}>
                  <input type="hidden" name="tier" value={tier.name} />
                  <Button type="submit" variant="secondary" aria-describedby={tierLabel} busy={pending.includes(`sync-${tier.name}`)}>
                    <span class="tier-copy"><bb-fit><span data-fit>{t('admin.deploys.flux.sync')}</span></bb-fit></span>
                  </Button>
                </form>
                <form
                  method="POST"
                  action={tier.suspended ? '?/fluxResume' : '?/fluxPause'}
                  use:enhance={submit(`suspend-${tier.name}`)}
                >
                  <input type="hidden" name="tier" value={tier.name} />
                  <Button type="submit" variant="ghost" aria-describedby={tierLabel} busy={pending.includes(`suspend-${tier.name}`)}>
                    <span class="tier-copy"><bb-fit><span data-fit>{t(tier.suspended ? 'admin.deploys.flux.resume' : 'admin.deploys.flux.pause')}</span></bb-fit></span>
                  </Button>
                </form>
              </Cluster>
            </li>
          {/each}
        </ul>
      {/if}

      <Stack gap={2}>
        <Heading level={4} as="h3">{t('admin.deploys.flux.pins')}</Heading>
        <Text size="sm" tone="muted">{t('admin.deploys.flux.pinsBody')}</Text>
        {#if pins.length === 0}
          <Text size="sm" tone="soft">{t('admin.deploys.flux.noPins')}</Text>
        {:else}
          <ul class="pins">
            {#each pins as pin (`${pin.tier}/${pin.name}`)}
              {@const pinLabel = `pin-${pin.tier}-${shortService(pin.name)}`}
              <li class="pin">
                <Tag tone="quiet" id={pinLabel}>{shortService(pin.name)}</Tag>
                <Text as="span" size="xs" mono truncate title={pin.newTag ?? pin.digest}>{pin.newTag ?? pin.digest}</Text>
                <form method="POST" action="?/fluxUnpin" use:enhance={submit(`unpin-${pin.name}`)}>
                  <input type="hidden" name="service" value={shortService(pin.name)} />
                  <Button type="submit" variant="ghost" aria-describedby={pinLabel} busy={pending.includes(`unpin-${pin.name}`)}>
                    {t('admin.deploys.flux.clear')}
                  </Button>
                </form>
              </li>
            {/each}
          </ul>
        {/if}
        <form class="pinform" method="POST" action="?/fluxPin" use:enhance={submit('pin')}>
          <Field fit label={t('admin.deploys.flux.service')} class="pin-field">
            <Select
              name="service"
              label={t('admin.deploys.flux.service')}
              placeholder={t('admin.deploys.flux.pickService')}
              options={services}
              bind:value={service}
              fill
            />
          </Field>
          <Field fit label={t('admin.deploys.flux.tag')} class="pin-field">
            <Input name="tag" bind:value={tag} mono required fill />
          </Field>
          <Button type="submit" busy={pending.includes('pin')} disabled={!service || !tag}>
            {t('admin.deploys.flux.pin')}
          </Button>
        </form>
      </Stack>
    </Stack>
  </Card>
</Scroller>

<style>
  :global(.deploy-flux-scroll) {
    container-type: inline-size;
    min-inline-size: 0;
    scrollbar-gutter: stable;
    overflow-wrap: anywhere;
  }

  .tiers,
  .pins {
    display: grid;
    gap: var(--bb-space-2);
    list-style: none;
    margin: 0;
    padding: 0;
  }
  .tier,
  .pin {
    display: grid;
    grid-template-columns: minmax(0, 1fr) minmax(0, 1fr) auto;
    align-items: center;
    gap: var(--bb-space-3);
    padding: var(--bb-space-2) var(--bb-space-3);
    border-radius: var(--bb-radius-sm);
    background: var(--bb-elev-1);
  }
  .name {
    display: flex;
    align-items: center;
    gap: var(--bb-space-1);
    min-width: 0;
  }

  .name :global(.tier-state) {
    min-inline-size: 0;
  }

  .tier-copy {
    display: block;
    inline-size: 5rem;
  }
  .pinform {
    display: grid;
    grid-template-columns: minmax(0, 1fr) minmax(0, 1fr) auto;
    align-items: end;
    gap: var(--bb-space-3);
  }

  .pinform > :global(.pin-field) {
    min-width: 0;
  }

  @container (max-width: 42rem) {
    .tier {
      grid-template-columns: minmax(0, 1fr) minmax(0, 1fr);
    }

    .tier > :global(.tier-actions) {
      grid-column: 1 / -1;
    }

    .pin {
      grid-template-columns: minmax(0, auto) minmax(0, 1fr) auto;
    }
  }

  @container (max-width: 29rem) {
    .pinform {
      grid-template-columns: minmax(0, 1fr);
    }
  }
</style>
