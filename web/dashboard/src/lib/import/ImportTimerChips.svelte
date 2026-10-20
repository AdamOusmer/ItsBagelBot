<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import Tag from '@bagel/ui/svelte/Tag.svelte';
  import { DEFAULT_CHAT_WINDOW_MINUTES, type ManifestTimer } from '@bagel/kit';
  import { translate, type Locale } from '@bagel/kit/i18n';

  let { timer, locale }: { timer: ManifestTimer; locale: Locale } = $props();
</script>

<Tag tone="bare">{translate(locale, 'import.everySeconds', { n: timer.interval_seconds })}</Tag>
{#if (timer.min_chat_lines ?? 0) > 0}
  <Tag tone="bare">{translate(locale, 'timers.pillMinLines', { n: timer.min_chat_lines ?? 0, m: timer.chat_window_minutes || DEFAULT_CHAT_WINDOW_MINUTES })}</Tag>
{/if}
{#if timer.online_only === false}<Tag tone="bare">{translate(locale, 'timers.pillOffline')}</Tag>{/if}
