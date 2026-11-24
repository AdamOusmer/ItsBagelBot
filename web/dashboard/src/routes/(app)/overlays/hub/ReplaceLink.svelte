<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  import { enhance, type SubmitFunction } from '$app/forms';
  import { getI18n } from '@bagel/kit/i18n/context';
  import Button from '@bagel/ui/svelte/Button.svelte';

  let { id, busy = $bindable(false), onLink, onError }: {
    id: number;
    busy?: boolean;
    onLink: (url: string) => void;
    onError: (message: string) => void;
  } = $props();

  const { t } = getI18n();

  function record(result: { type: string; data?: Record<string, unknown> }) {
    if (result.type === 'success' && typeof result.data?.url === 'string') onLink(result.data.url);
    else if (result.type !== 'success') onError(typeof result.data?.listError === 'string' ? result.data.listError : t('overlays.replaceFailed'));
  }

  const submit: SubmitFunction = async ({ cancel }) => {
    if (busy) return cancel();
    busy = true;
    return async ({ result }) => {
      try {
        record(result);
      } finally {
        busy = false;
      }
    };
  };
</script>

<form method="POST" action="?/rotate" use:enhance={submit}>
  <input type="hidden" name="id" value={id} />
  <Button type="submit" variant="ghost" size="sm" {busy}>
    <bb-fit><span data-fit="">{t('overlays.replace')}</span></bb-fit>
  </Button>
</form>
