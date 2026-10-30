<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import { untrack } from 'svelte';
  import { enhance, type SubmitFunction } from '$app/forms';
  import AlertBanner from '@bagel/ui/svelte/AlertBanner.svelte';
  import Button from '@bagel/ui/svelte/Button.svelte';
  import Card from '@bagel/ui/svelte/Card.svelte';
  import Cluster from '@bagel/ui/svelte/Cluster.svelte';
  import Heading from '@bagel/ui/svelte/Heading.svelte';
  import Input from '@bagel/ui/svelte/Input.svelte';
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

  let tiers = $state<FluxTier[]>(untrack(() => flux.tiers));
  let busy = $state<string | null>(null);
  let service = $state('');
  let tag = $state('');

  const pins = $derived(tiers.flatMap((tier) => tier.images.map((image) => ({ tier: tier.name, ...image }))));

  function tone(tier: FluxTier) {
    if (tier.suspended) return 'warning';
    if (tier.ready === null) return 'neutral';
    return tier.ready ? 'success' : 'danger';
  }

  function statusLabel(tier: FluxTier): string {
    if (tier.suspended) return t('admin.deploys.flux.paused');
    if (tier.ready === null) return t('admin.deploys.flux.unknown');
    return t(tier.ready ? 'admin.deploys.flux.ready' : 'admin.deploys.flux.notReady');
  }

  function shortRevision(revision: string): string {
    const sha = revision.split(':').pop() ?? '';
    return sha.slice(0, 12);
  }

  function shortService(image: string): string {
    return image.split('/').pop() ?? image;
  }

  function submit(key: string): SubmitFunction {
    return () => {
      busy = key;
      return async ({ result }) => {
        busy = null;
        const p = actionPayload<{ flux?: FluxResult; error?: string }>(result);
        if (p?.flux) {
          tiers = p.flux.tiers;
          if (key === 'pin') tag = '';
          return;
        }
        failed(p, t('admin.deploys.actionFailed'));
      };
    };
  }
</script>

<Card as="section" aria-labelledby="flux-title">
  <Stack gap={4}>
    <Stack gap={1}>
      <Heading level={3} as="h2" id="flux-title">{t('admin.deploys.flux.title')}</Heading>
      <Text size="sm" tone="muted">{t('admin.deploys.flux.body')}</Text>
    </Stack>

    {#if flux.fluxError}
      <AlertBanner>{t('admin.deploys.flux.error', { error: flux.fluxError })}</AlertBanner>
    {/if}

    <ul class="tiers">
      {#each tiers as tier (tier.name)}
        <li class="tier">
          <div class="name">
            <StatusDot tone={tone(tier)} />
            <Text as="span" mono>{tier.name}</Text>
            <Text as="span" size="xs" tone="muted">{statusLabel(tier)}</Text>
          </div>
          <Text as="span" size="xs" mono tone="soft" truncate title={tier.ready === false ? tier.message : undefined}>
            {tier.ready === false ? tier.reason : shortRevision(tier.revision)}
          </Text>
          <Cluster gap={2}>
            <form method="POST" action="?/fluxSync" use:enhance={submit(`sync-${tier.name}`)}>
              <input type="hidden" name="tier" value={tier.name} />
              <Button type="submit" size="sm" variant="secondary" busy={busy === `sync-${tier.name}`}>
                {t('admin.deploys.flux.sync')}
              </Button>
            </form>
            <form
              method="POST"
              action={tier.suspended ? '?/fluxResume' : '?/fluxPause'}
              use:enhance={submit(`suspend-${tier.name}`)}
            >
              <input type="hidden" name="tier" value={tier.name} />
              <Button type="submit" size="sm" variant="ghost" busy={busy === `suspend-${tier.name}`}>
                {t(tier.suspended ? 'admin.deploys.flux.resume' : 'admin.deploys.flux.pause')}
              </Button>
            </form>
          </Cluster>
        </li>
      {/each}
    </ul>

    <Stack gap={2}>
      <Heading level={4} as="h3">{t('admin.deploys.flux.pins')}</Heading>
      <Text size="sm" tone="muted">{t('admin.deploys.flux.pinsBody')}</Text>
      {#if pins.length === 0}
        <Text size="sm" tone="soft">{t('admin.deploys.flux.noPins')}</Text>
      {:else}
        <ul class="pins">
          {#each pins as pin (pin.name)}
            <li class="pin">
              <Tag tone="quiet">{shortService(pin.name)}</Tag>
              <Text as="span" size="xs" mono truncate>{pin.newTag ?? pin.digest}</Text>
              <form method="POST" action="?/fluxUnpin" use:enhance={submit(`unpin-${pin.name}`)}>
                <input type="hidden" name="service" value={shortService(pin.name)} />
                <Button type="submit" size="sm" variant="ghost" busy={busy === `unpin-${pin.name}`}>
                  {t('admin.deploys.flux.clear')}
                </Button>
              </form>
            </li>
          {/each}
        </ul>
      {/if}
      <form class="pinform" method="POST" action="?/fluxPin" use:enhance={submit('pin')}>
        <div class="field">
          <Select
            name="service"
            label={t('admin.deploys.flux.service')}
            placeholder={t('admin.deploys.flux.pickService')}
            options={flux.pinnable.map((name) => ({ value: name, label: name }))}
            bind:value={service}
          />
        </div>
        <label class="field">
          <Text as="span" size="xs" tone="muted">{t('admin.deploys.flux.tag')}</Text>
          <Input name="tag" bind:value={tag} mono required />
        </label>
        <Button type="submit" size="sm" busy={busy === 'pin'} disabled={!service || !tag}>
          {t('admin.deploys.flux.pin')}
        </Button>
      </form>
    </Stack>
  </Stack>
</Card>

<style>
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
    gap: var(--bb-space-2);
    min-width: 0;
  }
  .pinform {
    display: flex;
    flex-wrap: wrap;
    align-items: end;
    gap: var(--bb-space-3);
  }
  .field {
    display: grid;
    gap: var(--bb-space-1);
    min-width: 0;
  }
  @media (max-width: 640px) {
    .tier,
    .pin {
      grid-template-columns: minmax(0, 1fr) auto;
    }
  }
</style>
