<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import Checkbox from '@bagel/ui/svelte/Checkbox.svelte';
  import Icon from '@bagel/ui/svelte/Icon.svelte';
  import IconButton from '@bagel/ui/svelte/IconButton.svelte';
  import Stack from '@bagel/ui/svelte/Stack.svelte';
  import { getI18n } from '../../lib/i18n/context';
  import { bucketOf, entryOf, patchEntry } from '../../lib/overlay-editor/alert-rules';
  import { ALERT_KINDS, type AlertKind, type Settings } from '../../lib/overlay-editor/defaults';

  let { kind, settings, update, onadvanced }: {
    kind: 'alertbox' | 'eventlist';
    settings: Settings;
    update: (settings: Settings) => void;
    onadvanced: (event: AlertKind) => void;
  } = $props();

  const { t } = getI18n();
  const bucket = $derived(bucketOf(kind));
  const isOn = (event: AlertKind): boolean => entryOf(settings, bucket, event).enabled !== false;
  const toggle = (event: AlertKind, enabled: boolean): void => update(patchEntry(settings, bucket, event, { enabled }));
</script>

<Stack gap={1}>
  {#each ALERT_KINDS as event (event)}
    <div class="rule">
      <Checkbox checked={isOn(event)} onchange={(change) => toggle(event, change.currentTarget.checked)}>{t(`overlayEditor.alert.${event}`)}</Checkbox>
      <IconButton label={t('overlayEditor.ui.advanced')} size="sm" tooltip onclick={() => onadvanced(event)}>
        <Icon name="gear" size={14} />
      </IconButton>
    </div>
  {/each}
</Stack>

<style>
  .rule { display: flex; align-items: center; justify-content: space-between; gap: 8px; min-height: 36px; }
</style>
