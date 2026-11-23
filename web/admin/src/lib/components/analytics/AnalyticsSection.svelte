<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import Heading from '@bagel/ui/svelte/Heading.svelte';
  import Stack from '@bagel/ui/svelte/Stack.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import type { Snippet } from 'svelte';
  import { getI18n } from '@bagel/kit/i18n/context';

  let { id, children }: { id: string; children: Snippet } = $props();
  const { t } = getI18n();
</script>

<section {id} aria-labelledby={`${id}-heading`} class="analytics-section" tabindex="-1">
  <Stack gap={4}>
    <Stack gap={1}>
      <Heading id={`${id}-heading`} level={2} variant="section">{t(`admin.analytics.section.${id}.title`)}</Heading>
      <Text as="p" size="sm" tone="muted">{t(`admin.analytics.section.${id}.question`)}</Text>
    </Stack>
    {@render children()}
  </Stack>
</section>

<style>
  .analytics-section {
    min-inline-size: 0;
    scroll-margin-block-start: var(--bb-space-9);
  }
</style>
