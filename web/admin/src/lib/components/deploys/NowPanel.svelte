<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import Card from '@bagel/ui/svelte/Card.svelte';
  import CardHead from '@bagel/ui/svelte/CardHead.svelte';
  import Cluster from '@bagel/ui/svelte/Cluster.svelte';
  import ProgressBar from '@bagel/ui/svelte/ProgressBar.svelte';
  import Stack from '@bagel/ui/svelte/Stack.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import TextLink from '@bagel/ui/svelte/TextLink.svelte';
  import { getI18n } from '@bagel/kit/i18n/context';
  import type { DeployStage } from '#lib/deploys/types';
  import { STEP_STATE_KEY, currentItem, queueTone, ratio, stageTone } from './view';

  let { stage }: { stage: DeployStage } = $props();

  const { t, locale } = getI18n();
  const number = new Intl.NumberFormat(locale);
  const items = $derived(stage.items ?? []);
  const current = $derived(currentItem(stage));
  const at = $derived(current ? items.indexOf(current) : -1);
  const total = $derived(number.format(items.length));
  const queue = $derived(items.map((item) => queueTone(item.state)));
  const shipped = $derived(items.filter((item) => item.state === 'succeeded').length / Math.max(items.length, 1));
  const stateLabel = $derived(t(stage.state === 'running' ? 'admin.deploys.run.nowBuilding' : STEP_STATE_KEY[stage.state]));
  const heading = $derived(stage.state === 'succeeded' ? t('admin.deploys.run.nowAllBuilt', { total })
    : `${stateLabel} · ${t('admin.deploys.run.nowOf', { n: number.format(at + 1), total })}`);
</script>

{#if current}
  <Card as="section" aria-label={heading}>
    <CardHead title={heading} headingLevel={3} />
    <Stack gap={3}>
      <ProgressBar value={shipped} size="sm" segments={queue} current={at} label={heading} />

      <Text size="sm" truncate title={current.label}>{current.label}</Text>
      <ProgressBar
        value={current.state === 'succeeded' ? 1 : (ratio(current.progress) ?? null)}
        tone={stageTone(current.state)}
        label={current.label}
        size="sm"
      />
      <Cluster justify="between" nowrap>
        <div class="job-count bb-fit-box">
          <Text as="span" size="xs" tone="muted" mono>
            <bb-fit><span data-fit>{t('admin.deploys.run.jobs', {
              done: number.format(current.progress.done),
              total: number.format(current.progress.total)
            })}</span></bb-fit>
          </Text>
        </div>
        <div class="job-link bb-fit-box" class:unavailable={!current.url} inert={!current.url}>
          <TextLink href={current.url ?? ''} variant="inline" external>
            <bb-fit><span data-fit>{t('admin.deploys.run.openJob')}</span></bb-fit>
          </TextLink>
        </div>
      </Cluster>
      {#if current.detail}
        <div class="job-name"><Text size="xs" tone="muted">{current.detail}</Text></div>
      {/if}
    </Stack>
  </Card>
{/if}

<style>
  .job-name {
    overflow-wrap: anywhere;
  }

  .job-count {
    flex: 1;
    min-inline-size: 0;
    max-inline-size: 12rem;
  }

  .job-link {
    inline-size: min(8rem, 40%);
    text-align: end;
    transition: opacity var(--bb-dur-base) ease;
  }

  .job-link.unavailable {
    opacity: 0;
  }

  @media (prefers-reduced-motion: reduce) {
    .job-link {
      transition: none;
    }
  }
</style>
