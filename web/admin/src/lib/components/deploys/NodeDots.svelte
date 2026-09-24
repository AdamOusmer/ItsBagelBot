<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import { getI18n } from '@bagel/kit/i18n/context';
  import type { NodePod } from '$lib/deploys/types';
  import CheckDot from './CheckDot.svelte';
  import { POD_PHASE_KEY, groupNodes, podTone } from './view';

  let { pods }: { pods: NodePod[] | undefined } = $props();

  const { t } = getI18n();

  const nodes = $derived(groupNodes(pods));

  const podLabel = (p: NodePod) =>
    t('admin.deploys.podDot', { pod: p.pod, phase: t(POD_PHASE_KEY[p.phase]) });
</script>

<span class="nodes">
  {#each nodes as n (n.node)}
    <span class="node" role="group" aria-label={t('admin.deploys.nodeDots', { node: n.node })}>
      <span class="name">{n.node}</span>
      {#each n.pods as p (p.pod)}
        <CheckDot tone={podTone(p.phase)} label={podLabel(p)} />
      {/each}
    </span>
  {/each}
</span>

<style>
  .nodes {
    display: inline-flex;
    align-items: center;
    gap: 12px;
    min-width: 0;
    overflow: hidden;
  }
  .node {
    display: inline-flex;
    align-items: center;
    gap: 3px;
  }
  .name {
    margin-right: 3px;
    font-family: var(--bb-font-mono);
    font-size: 10.5px;
    color: var(--bb-muted);
  }
</style>
