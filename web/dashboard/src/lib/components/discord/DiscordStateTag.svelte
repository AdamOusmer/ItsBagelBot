<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import { Tag, getI18n, type GuildBotState } from '@bagel/kit';
  import { DISCORD_PILL_KEYS, DISCORD_STATE_TAG } from '$lib/discord-messages';

  let { state }: { state: GuildBotState } = $props();

  const { t } = getI18n();
  const look = $derived(DISCORD_STATE_TAG[state]);
</script>

<Tag tone={look.tone} mark={look.mark}>{t(DISCORD_PILL_KEYS[state])}</Tag>
