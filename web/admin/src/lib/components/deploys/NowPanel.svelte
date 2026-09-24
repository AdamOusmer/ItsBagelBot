<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import ProgressBar from '@bagel/ui/svelte/ProgressBar.svelte';
  import { getI18n } from '@bagel/kit/i18n/context';
  import type { DeployStage } from '$lib/deploys/types';
  import { flight } from './flight';
  import { POD_PHASE_KEY, currentItem, groupNodes, ratio, stageTone } from './view';

  let { stage }: { stage: DeployStage } = $props();

  const { t } = getI18n();
  const { arrive, depart } = flight(() => 1);

  const rolling = $derived(stage.id === 'rollout');
  const items = $derived(stage.items ?? []);
  const current = $derived(currentItem(stage));
  const at = $derived(current ? items.indexOf(current) : -1);
  const finished = $derived(stage.state === 'succeeded');
  const nodes = $derived(groupNodes(current?.nodes));

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
  <section class="now" aria-label={heading()}>
    <p class="kicker" class:done={finished}>{heading()}</p>

    <ol class="queue" aria-hidden="true">
      {#each items as item, i (item.key)}
        <li class="seg {item.state}" class:at={i === at}></li>
      {/each}
    </ol>

    <div class="stagebox">
      {#key current.key}
        <div class="current" in:arrive={{ i: 0 }} out:depart={{ i: 0 }}>
          <h3 class="name">{current.label}</h3>

          {#if rolling}
            <div class="lanes">
              {#each nodes as n (n.node)}
                {@const live = n.pods.some((p) => p.phase === 'pending')}
                <div class="lane" class:live>
                  <span class="node">{n.node}</span>
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
          <p class="caption">
            <span>{caption()}</span>
            {#if current.url}
              <a href={current.url} target="_blank" rel="noopener noreferrer">{t('admin.deploys.run.openJob')}</a>
            {/if}
          </p>
          {#if current.detail}<p class="detail">{current.detail}</p>{/if}
        </div>
      {/key}
    </div>
  </section>
{/if}

<style>
  .now {
    display: grid;
    gap: 12px;
    padding: 14px 16px 16px;
    background: linear-gradient(180deg, rgba(255, 255, 255, 0.05), rgba(0, 0, 0, 0.32));
    border: 1px solid var(--bb-border);
    border-radius: var(--bb-radius-md);
    box-shadow:
      inset 0 1px 0 rgba(255, 255, 255, 0.07),
      0 18px 40px rgba(0, 0, 0, 0.3);
    backdrop-filter: blur(10px);
  }
  .kicker {
    margin: 0;
    font-family: var(--bb-font-mono);
    font-size: 11px;
    letter-spacing: 0.12em;
    text-transform: uppercase;
    color: var(--bb-green-glow);
  }
  .kicker.done {
    color: var(--bb-tan);
  }

  .queue {
    display: grid;
    grid-auto-flow: column;
    grid-auto-columns: minmax(0, 1fr);
    gap: 4px;
    margin: 0;
    padding: 0;
    list-style: none;
  }
  .seg {
    height: 4px;
    border-radius: var(--bb-radius-pill);
    background: rgba(255, 255, 255, 0.1);
    transition: background 400ms var(--bb-ease-out-expo);
  }
  .seg.succeeded {
    background: var(--bb-tan);
  }
  .seg.running,
  .seg.waiting {
    background: var(--bb-green-glow);
  }
  .seg.failed {
    background: var(--bb-status-error, #e5484d);
  }
  .seg.at {
    box-shadow: 0 0 10px rgba(var(--bb-green-glow-rgb), 0.6);
  }

  .stagebox {
    display: grid;
    overflow: hidden;
  }
  .current {
    grid-area: 1 / 1;
    display: grid;
    gap: 12px;
    min-width: 0;
  }
  .name {
    margin: 2px 0 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font: 700 22px/1.15 var(--bb-font-display);
    letter-spacing: -0.02em;
    color: var(--bb-white);
  }

  .lanes {
    display: grid;
    gap: 8px;
  }
  .lane {
    position: relative;
    display: grid;
    grid-template-columns: minmax(0, 1fr) auto;
    align-items: center;
    gap: 10px;
    height: 30px;
    padding: 0 10px;
    overflow: hidden;
    border: 1px solid var(--bb-border);
    border-radius: var(--bb-radius-sm);
    background: rgba(0, 0, 0, 0.18);
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
    animation: sweep 1.8s var(--bb-ease-out-expo) infinite;
    pointer-events: none;
  }
  .node {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-family: var(--bb-font-mono);
    font-size: 11.5px;
    color: rgba(255, 255, 255, 0.75);
  }
  .pods {
    display: inline-flex;
    gap: 5px;
  }
  .pod {
    width: 22px;
    height: 10px;
    border-radius: var(--bb-radius-pill);
    border: 1px solid var(--bb-border-strong);
    transition:
      background 500ms var(--bb-ease-out-expo),
      border-color 500ms var(--bb-ease-out-expo);
  }
  .pod.new {
    background: var(--bb-green-glow);
    border-color: var(--bb-green-glow);
    animation: land 700ms var(--bb-ease-out-expo);
  }
  .pod.pending {
    border-color: var(--bb-green-glow);
    animation: pulse 1.2s ease-in-out infinite;
  }
  .pod.failing {
    background: var(--bb-status-error, #e5484d);
    border-color: var(--bb-status-error, #e5484d);
  }

  .jobs {
    display: grid;
    grid-auto-flow: column;
    grid-auto-columns: minmax(0, 1fr);
    gap: 6px;
  }
  .job {
    position: relative;
    height: 26px;
    overflow: hidden;
    border: 1px solid var(--bb-border-strong);
    border-radius: var(--bb-radius-sm);
    transition:
      background 500ms var(--bb-ease-out-expo),
      border-color 500ms var(--bb-ease-out-expo);
  }
  .job.done {
    background: linear-gradient(100deg, var(--bb-tan), rgba(var(--bb-green-glow-rgb), 0.8));
    border-color: transparent;
    animation: land 700ms var(--bb-ease-out-expo);
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
    animation: sweep 1.4s var(--bb-ease-out-expo) infinite;
  }

  .caption {
    display: flex;
    justify-content: space-between;
    gap: 10px;
    margin: 0;
    font-family: var(--bb-font-mono);
    font-size: 11.5px;
    color: rgba(255, 255, 255, 0.75);
    font-variant-numeric: tabular-nums;
  }
  .caption a {
    color: var(--bb-tan-pale);
  }
  .detail {
    margin: 0;
    font-size: 12.5px;
    color: var(--bb-muted);
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
    .seg,
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
