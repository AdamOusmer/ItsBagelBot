<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import RadioGroup from '@bagel/ui/svelte/RadioGroup.svelte';
  import EmptyState from '@bagel/ui/svelte/EmptyState.svelte';
  import { getI18n } from '@bagel/kit/i18n/context';
  import type { DeployReleaseInfo } from '#lib/deploys/types';
  import { shortSha } from './view';

  let { releases, value = $bindable('') }: { releases: DeployReleaseInfo[]; value: string } = $props();

  const { t, locale } = getI18n();
  const dates = new Intl.DateTimeFormat(locale, { dateStyle: 'medium', timeZone: 'UTC' });

  function publishedAt(value: string): string {
    const date = new Date(value);
    return Number.isNaN(date.getTime()) ? '-' : dates.format(date);
  }

  const options = $derived(
    releases.map((release) => ({
      value: release.version,
      label: release.version,
      description: shortSha(release.sha),
      meta: publishedAt(release.published_at)
    }))
  );
</script>

{#if releases.length === 0}
  <EmptyState title={t('admin.deploys.flow.releasesEmpty')} />
{:else}
  <RadioGroup variant="rows" name="ship-release" bind:value {options} label={t('admin.deploys.rollbackTo')} />
{/if}
