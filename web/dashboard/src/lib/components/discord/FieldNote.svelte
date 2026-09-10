<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  // The mark under a control whose value a save refused.
  //
  // It is a row-level note rather than a toast because the reseed that follows
  // an `invalid` refusal snaps the control back to the stored value: without a
  // mark, nothing on screen would say which control moved or why.
  import { FieldError, getI18n, type DiscordConfig, type RefusedFields } from '@bagel/kit';

  let { invalid, field }: { invalid: RefusedFields; field: keyof DiscordConfig } = $props();
  const { t } = getI18n();
</script>

{#if invalid.byField[field]}
  <span class="field-note"><FieldError message={t('discord.fieldInvalid')} /></span>
{/if}
