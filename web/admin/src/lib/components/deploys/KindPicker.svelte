<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import Eyebrow from '@bagel/ui/svelte/Eyebrow.svelte';
  import RadioGroup from '@bagel/ui/svelte/RadioGroup.svelte';
  import { getI18n } from '@bagel/kit/i18n/context';
  import { RUN_KINDS, STAGES_FOR } from '$lib/deploys/types';
  import type { RunKind } from '$lib/deploys/types';
  import { KIND_HINT_KEY, KIND_KEY } from './view';

  let { kind, onpick }: { kind: RunKind; onpick: (k: RunKind) => void } = $props();

  const { t } = getI18n();

  const options = $derived(
    RUN_KINDS.map((k) => ({
      value: k,
      label: t(KIND_KEY[k]),
      description: t(KIND_HINT_KEY[k]),
      meta: t('admin.deploys.flow.stagesCount', { n: String(STAGES_FOR[k].length) })
    }))
  );
  const position = (value: string) => String(RUN_KINDS.indexOf(value as RunKind) + 1).padStart(2, '0');
</script>

<RadioGroup
  variant="cards"
  cols={5}
  rail="md"
  min="168px"
  --choice-min-h="196px"
  name="ship-kind"
  value={kind}
  {options}
  label={t('admin.deploys.kindLabel')}
  onSelect={(value) => onpick(value as RunKind)}
>
  {#snippet leading(option)}<Eyebrow aria-hidden="true">{position(option.value)}</Eyebrow>{/snippet}
</RadioGroup>
