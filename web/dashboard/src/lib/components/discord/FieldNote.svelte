<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import { FieldError, getI18n, type DiscordConfig, type RefusedFields } from '@bagel/kit';

  let { invalid, field }: { invalid: RefusedFields; field: keyof DiscordConfig } = $props();
  const { t } = getI18n();
</script>

{#if invalid.byField[field]}
  <span class="field-note"><FieldError message={t('discord.fieldInvalid')} /></span>
{/if}
