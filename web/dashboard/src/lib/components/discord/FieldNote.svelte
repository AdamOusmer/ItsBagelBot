<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import { FieldError, getI18n, type DiscordConfig, type RefusedFields } from '@bagel/kit';

  let {
    invalid,
    field,
    warning = '',
    quiet = false
  }: { invalid: RefusedFields; field: keyof DiscordConfig; warning?: string; quiet?: boolean } = $props();
  const { t } = getI18n();
</script>

{#if invalid.byField[field]}
  <span class="field-note"><FieldError message={t('discord.fieldInvalid')} /></span>
{:else if warning}
  <span class="field-note" data-quiet={quiet || undefined}><FieldError message={warning} /></span>
{/if}
