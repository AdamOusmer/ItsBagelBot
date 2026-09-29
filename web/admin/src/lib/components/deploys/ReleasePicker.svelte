<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import RadioGroup from '@bagel/ui/svelte/RadioGroup.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import { ago } from '@bagel/kit';
  import { getI18n } from '@bagel/kit/i18n/context';
  import type { DeployReleaseInfo } from '$lib/deploys/types';
  import { shortSha } from './view';

  let { releases, value = $bindable('') }: { releases: DeployReleaseInfo[]; value: string } = $props();

  const { t } = getI18n();

  const options = $derived(
    releases.map((r) => ({ value: r.version, label: r.version, description: shortSha(r.sha), meta: ago(r.published_at) }))
  );
</script>

{#if releases.length === 0}
  <Text size="sm" tone="muted">{t('admin.deploys.flow.releasesEmpty')}</Text>
{:else}
  <RadioGroup variant="rows" maxHeight="360px" name="ship-release" bind:value {options} label={t('admin.deploys.rollbackTo')} />
{/if}
