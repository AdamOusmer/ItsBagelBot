<script lang="ts">
  // Custom-cursor preference toggle. Binds to the shared `customCursor` store so
  // flipping it changes the cursor live (no reload), then POSTs to /cursor to
  // persist the choice to the account + preference cookie. A persistence failure
  // is surfaced as a toast; a network failure (nothing stored) reverts the store
  // so the control keeps matching reality.
  import { Switch, customCursor, getI18n, toast } from '@bagel/shared';

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
      if (!res.ok) toast('err', t('settings.cursorSaveError'));
    } catch {
      customCursor.set(prev);
      toast('err', t('settings.cursorSaveError'));
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
