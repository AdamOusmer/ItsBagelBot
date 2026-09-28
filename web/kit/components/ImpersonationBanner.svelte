<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import AlertBanner from '@bagel/ui/svelte/AlertBanner.svelte';
  import type { Snippet } from 'svelte';
  import { getI18n } from '../lib/i18n/context';

  const { t } = getI18n();

  let { exitHref, exitForm = false, exitLabel = t('banner.exit'), second = false, children }:
    { exitHref?: string; exitForm?: boolean; exitLabel?: string; second?: boolean; children: Snippet } = $props();

  const action = $derived(exitForm ? { label: exitLabel, formAction: '/auth/logout' } : { label: exitLabel, href: exitHref ?? '/' });
</script>

<AlertBanner tone="warm" placement="top" row={second ? 2 : 1} role="status" {action}>{@render children()}</AlertBanner>
