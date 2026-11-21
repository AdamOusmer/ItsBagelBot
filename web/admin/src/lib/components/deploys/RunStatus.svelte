<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import StatusDot from '@bagel/ui/svelte/StatusDot.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import { getI18n } from '@bagel/kit/i18n/context';
  import { livePoll } from '@bagel/kit/live-poll';
  import StatePill from '#lib/components/StatePill.svelte';
  import type { DeployRun } from '#lib/deploys/types';
  import type { StreamConn } from './run-stream.svelte';
  import { RUN_STATE_KEY, formatElapsed, isTerminal, runElapsed, runPill } from './view';

  let { run, conn }: { run: DeployRun; conn: StreamConn } = $props();
  const { t } = getI18n();

  const CONNECTION = {
    connecting: { key: 'admin.deploys.streamConnecting', tone: 'warning' },
    live: { key: 'admin.deploys.streamLive', tone: 'success' },
    reconnecting: { key: 'admin.deploys.streamReconnecting', tone: 'warning' }
  } as const;

  let now = $state<number>();
  const connection = $derived(CONNECTION[conn]);
  const terminal = $derived(isTerminal(run.state));
  const elapsed = $derived(formatElapsed(runElapsed(run, now ?? Date.parse(run.updated_at))));

  function refreshClock() {
    if (!document.hidden) now = Date.now();
  }

  $effect(() => {
    if (terminal) return;
    refreshClock();
    return livePoll(async () => {
      refreshClock();
      return false;
    }, {
      firstDelayMs: 1000,
      delayMs: () => 1000,
      timeoutMs: Infinity,
      hiddenDelayMs: 60_000,
      refreshOnVisible: true
    });
  });
</script>

<div class="run-status">
  <div class="state bb-fit-box" role="status">
    <StatePill tone={runPill(run.state)}>
      <bb-fit><span data-fit>{t(RUN_STATE_KEY[run.state])}</span></bb-fit>
    </StatePill>
  </div>
  <Text as="span" size="xs" mono>
    <bb-fit><span data-fit>{t('admin.deploys.elapsed', { time: elapsed })}</span></bb-fit>
  </Text>
  <div class="connection" role="status">
    <StatusDot tone={connection.tone} />
    <Text as="span" size="xs" tone="muted">
      <bb-fit><span data-fit>{t(connection.key)}</span></bb-fit>
    </Text>
  </div>
</div>

<style>
  .run-status {
    display: grid;
    grid-template-columns: minmax(0, 1.3fr) minmax(0, 1fr) minmax(0, 1fr);
    align-items: center;
    gap: var(--bb-space-2);
    inline-size: min(100%, 25rem);
    min-inline-size: 0;
    min-block-size: 2.75rem;
    font-variant-numeric: tabular-nums;
  }
  .state {
    display: grid;
    justify-items: start;
    min-width: 0;
  }
  .connection {
    display: grid;
    grid-template-columns: auto minmax(0, 1fr);
    align-items: center;
    gap: var(--bb-space-2);
  }
</style>
