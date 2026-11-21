<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import RadioGroup from '@bagel/ui/svelte/RadioGroup.svelte';
  import { getI18n } from '@bagel/kit/i18n/context';
  import { RUN_KINDS, STAGES_FOR } from '#lib/deploys/types';
  import type { RunKind } from '#lib/deploys/types';
  import { KIND_HINT_KEY, KIND_KEY } from './view';

  let {
    kind,
    disabled,
    onpick
  }: {
    kind: RunKind;
    disabled: boolean;
    onpick: (kind: RunKind) => void;
  } = $props();

  const { t, locale } = getI18n();
  const number = new Intl.NumberFormat(locale);

  const options = $derived(
    RUN_KINDS.map((k) => ({
      value: k,
      label: t(KIND_KEY[k]),
      description: t(KIND_HINT_KEY[k]),
      meta: t('admin.deploys.flow.stagesCount', { n: number.format(STAGES_FOR[k].length) }),
      disabled
    }))
  );
</script>

<RadioGroup
  variant="cards"
  rail="md"
  min="9.5rem"
  --choice-min-h="11.25rem"
  name="ship-kind"
  value={kind}
  {options}
  label={t('admin.deploys.kindLabel')}
  onSelect={(value) => onpick(value as RunKind)}
/>
