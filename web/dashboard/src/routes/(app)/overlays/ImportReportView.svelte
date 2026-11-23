<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import { getI18n } from '@bagel/kit/i18n/context';
  import { importReason, REPORT_GROUPS, type ImportItem, type ImportReport, type ReportGroup } from '@bagel/kit/overlay-editor';
  import Badge from '@bagel/ui/svelte/Badge.svelte';
  import Stack from '@bagel/ui/svelte/Stack.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';

  let { report }: { report: ImportReport } = $props();

  const { t } = getI18n();
  const SHOWN = 50;
  const TONES = { mapped: 'success', approximated: 'warning', unsupported: 'danger' } as const satisfies Record<ReportGroup, string>;

  function line(item: ImportItem): string {
    if (!item.reason) return item.subject;
    const reason = importReason(item.reason) ?? 'unknown';
    return `${item.subject}: ${t(`overlayEditor.reason.${reason}`)}`;
  }
</script>

<Stack gap={3}>
  {#each REPORT_GROUPS as group (group)}
    {@const items = report[group]}
    <Stack gap={2}>
      <div><Badge tone={TONES[group]}>{t(`overlayEditor.import.${group}`)} {items.length}</Badge></div>
      <ul class="items">
        {#each items.slice(0, SHOWN) as item, index (`${item.subject}:${index}`)}
          <li class="item"><Text as="span" size="xs" tone="muted">{line(item)}</Text></li>
        {/each}
        {#if items.length > SHOWN}
          <li class="item"><Text as="span" size="xs" tone="muted">{t('overlayEditor.import.more', { count: items.length - SHOWN })}</Text></li>
        {/if}
      </ul>
    </Stack>
  {/each}
</Stack>

<style>
  .items { display: grid; gap: var(--bb-space-1); max-block-size: 12rem; margin: 0; padding: 0; overflow: auto; list-style: none; }
  .item { min-inline-size: 0; overflow-wrap: anywhere; }
</style>
