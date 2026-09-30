<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import { Switch, toast } from '@bagel/ui/svelte';
  import { customCursor, getI18n } from '@bagel/kit';

  let { describedby }: { describedby?: string } = $props();

  const { t } = getI18n();
  let pending = $state(false);

  async function persist(on: boolean) {
    const prev = !on;
    pending = true;
    try {
      const res = await fetch('/cursor', {
        method: 'POST',
        headers: { 'content-type': 'application/json' },
        body: JSON.stringify({ enabled: on })
      });
      if (!res.ok) throw new Error(String(res.status));
    } catch {
      customCursor.set(prev);
      toast('danger', t('settings.cursorSaveError'));
    } finally {
      pending = false;
    }
  }
</script>

<Switch
  bind:checked={$customCursor}
  {pending}
  label={t('settings.customCursor')}
  {describedby}
  onchange={persist}
/>
