<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  // Where the run stands in one glance: its state, how long it has taken, who
  // started it, whether this page is still receiving it, and one bar for the
  // whole train. The stream state is shown because EventSource retries
  // silently; a page that stopped updating must not look like a stalled run.
  import ProgressBar from '@bagel/ui/svelte/ProgressBar.svelte';
  import StatusDot from '@bagel/ui/svelte/StatusDot.svelte';
  import { getI18n } from '@bagel/kit/i18n/context';
  import type { DeployRun } from '$lib/deploys/types';
  import StatePill from '$lib/components/StatePill.svelte';
  import type { StreamConn } from './run-stream.svelte';
  import { RUN_STATE_KEY, formatElapsed, runElapsed, runFraction, runPill, runTone, type Tone } from './view';

  let { run, conn, now }: { run: DeployRun; conn: StreamConn; now: number } = $props();

  const { t } = getI18n();

  const CONN: Record<StreamConn, { key: string; tone: Tone }> = {
    live: { key: 'admin.deploys.streamLive', tone: 'success' },
    connecting: { key: 'admin.deploys.streamConnecting', tone: 'warning' },
    reconnecting: { key: 'admin.deploys.streamReconnecting', tone: 'warning' }
  };
</script>

<div class="head">
  <div class="line">
    <StatePill tone={runPill(run.state)}>{t(RUN_STATE_KEY[run.state])}</StatePill>
    <span class="fact">{t('admin.deploys.elapsed', { time: formatElapsed(runElapsed(run, now)) })}</span>
    <span class="fact">{t('admin.deploys.startedBy', { login: run.actor.login })}</span>
    <span class="conn"><StatusDot tone={CONN[conn].tone} />{t(CONN[conn].key)}</span>
  </div>
  <ProgressBar value={runFraction(run)} tone={runTone(run.state)} label={t('admin.deploys.overall')} />
</div>

<style>
  .head {
    display: flex;
    flex-direction: column;
    gap: 12px;
    margin-bottom: 16px;
  }
  .line {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 16px;
    min-height: 28px;
  }
  .fact,
  .conn {
    font-family: var(--bb-font-mono);
    font-size: 11.5px;
    color: var(--bb-muted);
    font-variant-numeric: tabular-nums;
  }
  .conn {
    display: inline-flex;
    align-items: center;
    gap: 7px;
    margin-left: auto;
  }
</style>
