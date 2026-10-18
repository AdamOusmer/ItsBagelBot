<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import AlertBanner from '@bagel/ui/svelte/AlertBanner.svelte';
  import { getI18n } from '@bagel/kit';
  import ModuleFields from '$lib/components/discord/ModuleFields.svelte';
  import ModuleShell from '$lib/components/discord/ModuleShell.svelte';
  import { createModuleDraft } from '$lib/discord/module-draft.svelte';
  import { layoutDownOf } from '$lib/discord/guild-view';

  let { data } = $props();
  const { t } = getI18n();

  const draft = createModuleDraft({ data: () => data, t });
</script>

{#if layoutDownOf(data.layout) && data.def.fields.length > 0}
  <AlertBanner tone="warning">{t('discord.layoutUnavailable')}</AlertBanner>
{/if}

<ModuleShell {draft} guildId={data.guildId}>
  <ModuleFields {draft} layout={data.layout} />
</ModuleShell>
