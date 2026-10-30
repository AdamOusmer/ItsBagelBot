<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import Card from '@bagel/ui/svelte/Card.svelte';
  import Eyebrow from '@bagel/ui/svelte/Eyebrow.svelte';
  import Heading from '@bagel/ui/svelte/Heading.svelte';
  import ProgressBar from '@bagel/ui/svelte/ProgressBar.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import TextLink from '@bagel/ui/svelte/TextLink.svelte';
  import { getI18n } from '@bagel/kit/i18n/context';
  import type { DeployStage } from '#lib/deploys/types';
  import { flight } from './flight';
  import { currentItem, queueTone, ratio, stageTone } from './view';

  let { stage }: { stage: DeployStage } = $props();

  const { t } = getI18n();
  const { arrive, depart } = flight(() => 1);

  const items = $derived(stage.items ?? []);
  const current = $derived(currentItem(stage));
  const at = $derived(current ? items.indexOf(current) : -1);
  const finished = $derived(stage.state === 'succeeded');
  const queue = $derived(items.map((item) => queueTone(item.state)));
  const shipped = $derived(items.filter((item) => item.state === 'succeeded').length / Math.max(items.length, 1));

  function heading(): string {
    const total = String(items.length);
    if (finished) return t('admin.deploys.run.nowAllBuilt', { total });
    return `${t('admin.deploys.run.nowBuilding')} · ${t('admin.deploys.run.nowOf', { n: String(at + 1), total })}`;
  }

  function caption(): string {
    if (!current) return '';
    return t('admin.deploys.run.jobs', { done: String(current.progress.done), total: String(current.progress.total) });
  }
</script>

{#if current}
  <Card as="section" flush aria-label={heading()}>
    <div class="now">
      <Eyebrow as="p" tone={finished ? 'default' : 'go'}>{heading()}</Eyebrow>

      <ProgressBar value={shipped} size="sm" segments={queue} current={at} label={heading()} aria-hidden="true" />

      <div class="stagebox">
        {#key current.key}
          <div class="current" in:arrive={{ i: 0 }} out:depart={{ i: 0 }}>
            <Heading level={3}><span class="name">{current.label}</span></Heading>

            <div class="jobs" aria-hidden="true">
              {#each Array.from({ length: current.progress.total }, (_, j) => j) as j (j)}
                <span
                  class="job"
                  class:done={j < current.progress.done}
                  class:live={j === current.progress.done && current.state === 'running'}
                ></span>
              {/each}
            </div>

            <ProgressBar
              value={current.state === 'succeeded' ? 1 : (ratio(current.progress) ?? null)}
              tone={stageTone(current.state)}
              label={current.label}
              size="sm"
            />
            <div class="caption">
              <Text as="span" size="xs" mono tone="soft">{caption()}</Text>
              {#if current.url}
                <TextLink href={current.url} label={t('admin.deploys.run.openJob')} external />
              {/if}
            </div>
            {#if current.detail}<Text size="xs" tone="muted">{current.detail}</Text>{/if}
          </div>
        {/key}
      </div>
    </div>
  </Card>
{/if}

<style>
  .now {
    display: grid;
    gap: var(--bb-space-3);
    padding: var(--bb-space-3) var(--bb-space-4) var(--bb-space-4);
  }
  .stagebox {
    display: grid;
    overflow: hidden;
  }
  .current {
    grid-area: 1 / 1;
    display: grid;
    gap: var(--bb-space-3);
    min-width: 0;
  }
  .name {
    display: block;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .jobs {
    display: grid;
    grid-auto-flow: column;
    grid-auto-columns: minmax(0, 1fr);
    gap: var(--bb-space-2);
  }
  .job {
    position: relative;
    height: 26px;
    overflow: hidden;
    border-radius: var(--bb-radius-sm);
    background: var(--bb-elev-1);
    box-shadow: var(--bb-depth-0);
    transition:
      background var(--bb-dur-slow) var(--bb-ease-out-expo),
      box-shadow var(--bb-dur-slow) var(--bb-ease-out-expo);
  }
  .job.done {
    background: linear-gradient(100deg, var(--bb-tan), rgba(var(--bb-green-glow-rgb), 0.8));
    box-shadow: var(--bb-depth-1);
    animation: land var(--bb-dur-slow) var(--bb-ease-out-expo);
  }
  .job.live {
    box-shadow:
      var(--bb-depth-0),
      inset 0 0 0 1px var(--bb-green-glow);
  }
  .job.live::after {
    content: '';
    position: absolute;
    inset: 0;
    background: linear-gradient(90deg, transparent, rgba(var(--bb-green-glow-rgb), 0.35), transparent);
    transform: translateX(-100%);
    animation: sweep calc(var(--bb-dur-slow) * 2.5) var(--bb-ease-out-expo) infinite;
  }

  .caption {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--bb-space-2);
    min-height: calc(var(--bb-text-xs) * 1.75);
    font-variant-numeric: tabular-nums;
  }

  @keyframes sweep {
    from {
      transform: translateX(-100%);
    }
    to {
      transform: translateX(100%);
    }
  }
  @keyframes land {
    from {
      transform: scale(0.6);
      opacity: 0.4;
    }
    to {
      transform: none;
      opacity: 1;
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .job {
      transition: none;
    }
    .job.live::after,
    .job.done {
      animation: none;
    }
  }
</style>
