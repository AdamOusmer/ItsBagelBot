<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  // The bot-state label, shared by the hub cards, the guild header, and the
  // overview facts. Four states, not a boolean: a guild whose install predates
  // a permission needs the streamer to act, and "offline" would send them to
  // wait for a reconnect that already happened -- while `unknown` is the
  // listing admitting it never read this guild's reauth flag, which must not
  // be painted as either health or fault (see guildBotState).
  //
  // This used to be a filled `.pill.{online,offline,reauth,unknown}` copy-
  // pasted on three surfaces. Status is a Tag now: system tones, diamond
  // mark, no radius-pill fill. The mark is the second channel so colour is
  // never the only difference (live+solid / error+solid / alpha+dash /
  // quiet+hollow).
  import { Tag, getI18n, type GuildBotState } from '@bagel/kit';
  import { DISCORD_PILL_KEYS, DISCORD_STATE_TAG } from '$lib/discord-messages';

  let { state }: { state: GuildBotState } = $props();

  const { t } = getI18n();
  const look = $derived(DISCORD_STATE_TAG[state]);
</script>

<Tag tone={look.tone} mark={look.mark}>{t(DISCORD_PILL_KEYS[state])}</Tag>
