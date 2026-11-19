<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import { getI18n } from '@bagel/kit';
  import Badge from '@bagel/ui/svelte/Badge.svelte';
  import Icon from '@bagel/ui/svelte/Icon.svelte';
  import type { StepOutcome } from '#lib/flow/rehearsal-view';

  const LOOKS = {
    passed: { icon: 'check', tone: 'free' },
    skipped: { icon: 'minus', tone: 'skipped' },
    issue: { icon: 'edit', tone: 'warning' },
    failed: { icon: 'x', tone: 'banned' },
    untested: { icon: 'clock', tone: 'inactive' }
  } as const;
  const LABELS = {
    passed: 'flows.test.outcome.passed',
    skipped: 'flows.test.outcome.skipped',
    issue: 'flows.test.outcome.issue',
    failed: 'flows.test.outcome.failed',
    untested: 'flows.test.outcome.untested'
  } as const;

  let { outcome }: { outcome: StepOutcome } = $props();
  const { t } = getI18n();
  const look = $derived(LOOKS[outcome]);
</script>

<Badge shape="status" tone={look.tone}>
  <Icon name={look.icon} size={14} />
  <span class="label bb-fit-box"><bb-fit>{t(LABELS[outcome])}</bb-fit></span>
</Badge>

<style>
  .label { width: calc(var(--bb-space-7) + var(--bb-space-4)); height: var(--bb-space-4); line-height: 1.34; }
</style>
