<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import { DEFAULT_CHAT_WINDOW_MINUTES, Tag } from '@bagel/kit';
  import type { ManifestTimer } from '@bagel/kit';
  import type { Translate } from './session.svelte';

  let { timer, t }: { timer: ManifestTimer; t: Translate } = $props();
</script>

<Tag tone="bare">{t('import.everySeconds', { n: timer.interval_seconds })}</Tag>
{#if (timer.min_chat_lines ?? 0) > 0}
  <Tag tone="bare">{t('timers.pillMinLines', { n: timer.min_chat_lines ?? 0, m: timer.chat_window_minutes || DEFAULT_CHAT_WINDOW_MINUTES })}</Tag>
{/if}
{#if timer.online_only === false}<Tag tone="bare">{t('timers.pillOffline')}</Tag>{/if}
