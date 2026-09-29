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
  import type { DeployStage } from '$lib/deploys/types';
  import { flight } from './flight';
  import { POD_PHASE_KEY, currentItem, groupNodes, queueTone, ratio, stageTone } from './view';

  let { stage }: { stage: DeployStage } = $props();

  const { t } = getI18n();
  const { arrive, depart } = flight(() => 1);

  const rolling = $derived(stage.id === 'rollout');
  const items = $derived(stage.items ?? []);
  const current = $derived(currentItem(stage));
  const at = $derived(current ? items.indexOf(current) : -1);
  const finished = $derived(stage.state === 'succeeded');
  const nodes = $derived(groupNodes(current?.nodes));
  const queue = $derived(items.map((item) => queueTone(item.state)));
  const shipped = $derived(items.filter((item) => item.state === 'succeeded').length / Math.max(items.length, 1));

  function heading(): string {
    const total = String(items.length);
    if (finished) return t(rolling ? 'admin.deploys.run.nowAllRolled' : 'admin.deploys.run.nowAllBuilt', { total });
    const verb = t(rolling ? 'admin.deploys.run.nowRolling' : 'admin.deploys.run.nowBuilding');
    return `${verb} · ${t('admin.deploys.run.nowOf', { n: String(at + 1), total })}`;
  }

  function caption(): string {
    if (!current) return '';
    const params = { done: String(current.progress.done), total: String(current.progress.total) };
    return t(rolling ? 'admin.deploys.run.pods' : 'admin.deploys.run.jobs', params);
  }

  const podLabel = (pod: string, phase: keyof typeof POD_PHASE_KEY) =>
    t('admin.deploys.podDot', { pod, phase: t(POD_PHASE_KEY[phase]) });
</script>

{#if current}
  <Card as="section" glass flush aria-label={heading()}>
    <div class="now">
      <Eyebrow as="p" tone={finished ? 'default' : 'go'}>{heading()}</Eyebrow>

      <ProgressBar value={shipped} size="sm" segments={queue} current={at} label={heading()} aria-hidden="true" />

      <div class="stagebox">
        {#key current.key}
          <div class="current" in:arrive={{ i: 0 }} out:depart={{ i: 0 }}>
            <Heading level={3}><span class="name">{current.label}</span></Heading>

            {#if rolling}
              <div class="lanes">
                {#each nodes as n (n.node)}
                  {@const live = n.pods.some((p) => p.phase === 'pending')}
                  <div class="lane" class:live>
                    <Text as="span" size="xs" mono tone="soft" truncate>{n.node}</Text>
                    <span class="pods">
                      {#each n.pods as p (p.pod)}
                        <span class="pod {p.phase}" title={podLabel(p.pod, p.phase)}></span>
                      {/each}
                    </span>
                  </div>
                {/each}
              </div>
            {:else}
              <div class="jobs" aria-hidden="true">
                {#each Array.from({ length: current.progress.total }, (_, j) => j) as j (j)}
                  <span
                    class="job"
                    class:done={j < current.progress.done}
                    class:live={j === current.progress.done && current.state === 'running'}
                  ></span>
                {/each}
              </div>
            {/if}

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

  .lanes {
    display: grid;
    gap: var(--bb-space-2);
  }
  .lane {
    position: relative;
    display: grid;
    grid-template-columns: minmax(0, 1fr) auto;
    align-items: center;
    gap: var(--bb-space-2);
    height: 30px;
    padding: 0 var(--bb-space-2);
    overflow: hidden;
    border: 1px solid var(--bb-border);
    border-radius: var(--bb-radius-sm);
    background: rgba(var(--bb-shadow-rgb), 0.18);
  }
  .lane.live {
    border-color: rgba(var(--bb-green-glow-rgb), 0.45);
  }
  .lane.live::after {
    content: '';
    position: absolute;
    inset: 0;
    background: linear-gradient(90deg, transparent, rgba(var(--bb-green-glow-rgb), 0.16), transparent);
    transform: translateX(-100%);
    animation: sweep calc(var(--bb-dur-slow) * 3) var(--bb-ease-out-expo) infinite;
    pointer-events: none;
  }
  .pods {
    display: inline-flex;
    gap: var(--bb-space-1);
  }
  .pod {
    width: 22px;
    height: 10px;
    border-radius: var(--bb-radius-pill);
    border: 1px solid var(--bb-border-strong);
    transition:
      background var(--bb-dur-slow) var(--bb-ease-out-expo),
      border-color var(--bb-dur-slow) var(--bb-ease-out-expo);
  }
  .pod.new {
    background: var(--bb-green-glow);
    border-color: var(--bb-green-glow);
    animation: land var(--bb-dur-slow) var(--bb-ease-out-expo);
  }
  .pod.pending {
    border-color: var(--bb-green-glow);
    animation: pulse calc(var(--bb-dur-slow) * 2) ease-in-out infinite;
  }
  .pod.failing {
    background: var(--bb-status-error);
    border-color: var(--bb-status-error);
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
    border: 1px solid var(--bb-border-strong);
    border-radius: var(--bb-radius-sm);
    transition:
      background var(--bb-dur-slow) var(--bb-ease-out-expo),
      border-color var(--bb-dur-slow) var(--bb-ease-out-expo);
  }
  .job.done {
    background: linear-gradient(100deg, var(--bb-tan), rgba(var(--bb-green-glow-rgb), 0.8));
    border-color: transparent;
    animation: land var(--bb-dur-slow) var(--bb-ease-out-expo);
  }
  .job.live {
    border-color: var(--bb-green-glow);
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
  @keyframes pulse {
    0%,
    100% {
      opacity: 0.45;
    }
    50% {
      opacity: 1;
    }
  }

  @media (prefers-reduced-motion: reduce) {
    .pod,
    .job {
      transition: none;
    }
    .lane.live::after,
    .job.live::after,
    .pod.new,
    .pod.pending,
    .job.done {
      animation: none;
    }
  }
</style>
