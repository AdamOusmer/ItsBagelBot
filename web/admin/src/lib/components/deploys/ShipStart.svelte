<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import { applyAction, enhance, type SubmitFunction } from '$app/forms';
  import { onDestroy } from 'svelte';
  import { getI18n } from '@bagel/kit/i18n/context';
  import { prefersReducedMotion } from '@bagel/ui/lib/motion-query';
  import ConfirmDialog from '@bagel/ui/svelte/ConfirmDialog.svelte';
  import type { DeployPRInfo } from '#lib/deploys/types';
  import { confirmParams, countKey, type ShipState } from './ship';
  import { CONFIRM_KEY } from './view';

  let {
    ship,
    changelog,
    prs,
    serviceCount,
    open = $bindable(false),
    leaving = $bindable(false),
    onFailure
  }: {
    ship: ShipState;
    changelog: string;
    prs: DeployPRInfo[];
    serviceCount: number;
    open: boolean;
    leaving: boolean;
    onFailure: (error: string) => void;
  } = $props();

  const { t, locale } = getI18n();
  const number = new Intl.NumberFormat(locale);
  const EXIT_MS = 640;

  let form: HTMLFormElement | undefined;
  let submitTimer: ReturnType<typeof setTimeout> | undefined;
  let confirmed = false;

  onDestroy(() => clearTimeout(submitTimer));

  function counted(key: string, count: number) {
    return t(countKey(count, key), { n: number.format(count) });
  }

  const confirmText = $derived.by(() => {
    const line = t(CONFIRM_KEY[ship.kind], confirmParams(ship, counted('admin.deploys.servicesCount', serviceCount)));
    return prs.length ? `${counted('admin.deploys.confirmPrs', prs.length)} ${line}` : line;
  });

  function confirm() {
    if (leaving) return;
    confirmed = true;
    open = false;
    leaving = true;
    submitTimer = setTimeout(() => form?.requestSubmit(), prefersReducedMotion() ? 0 : EXIT_MS);
  }

  // Only the confirmation dialog can submit a production deploy.
  const submit: SubmitFunction = ({ cancel }) => {
    if (!confirmed) return cancel();
    confirmed = false;
    return async ({ result }) => {
      if (result.type !== 'failure') return applyAction(result);
      leaving = false;
      onFailure((result.data as { error?: string } | undefined)?.error ?? t('admin.deploys.startFailed'));
    };
  };
</script>

<form method="POST" action="/deploys?/start" use:enhance={submit} bind:this={form} hidden>
  <input type="hidden" name="kind" value={ship.kind} />
  <input type="hidden" name="version" value={ship.version} disabled={!ship.uses.version} />
  <input type="hidden" name="target_sha" value={ship.targetSha} disabled={!ship.uses.target} />
  <input type="hidden" name="rollback_to" value={ship.rollbackTo} disabled={!ship.uses.rollback} />
  <input type="hidden" name="changelog" value={changelog} disabled={!ship.uses.changelog} />
  {#each prs as pr (pr.number)}
    <input type="hidden" name="pr" value={String(pr.number)} />
  {/each}
</form>

<ConfirmDialog
  bind:open
  title={t('admin.deploys.confirmTitle')}
  body={confirmText}
  confirmLabel={t('admin.deploys.confirmStart')}
  cancelLabel={t('admin.deploys.confirmCancel')}
  onConfirm={confirm}
/>
