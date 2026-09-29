<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import { invalidateAll } from '$app/navigation';
  import Button from '@bagel/ui/svelte/Button.svelte';
  import { getI18n } from '@bagel/kit/i18n/context';

  const { t } = getI18n();

  let { class: className = '' }: { class?: string } = $props();

  let busy = $state(false);

  async function retry() {
    if (busy) return;
    busy = true;
    try {
      await invalidateAll();
    } finally {
      busy = false;
    }
  }
</script>

<Button variant="ghost" type="button" class={className} busy={busy} onclick={retry}>{t('overview.retry')}</Button>
