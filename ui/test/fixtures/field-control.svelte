<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  import Checkbox from '../../svelte/Checkbox.svelte';
  import Field from '../../svelte/Field.svelte';
  import Input from '../../svelte/Input.svelte';
  import Select from '../../svelte/Select.svelte';
  import Slider from '../../svelte/Slider.svelte';
  import Textarea from '../../svelte/Textarea.svelte';

  let { kind }: { kind: string } = $props();
  const options = [{ value: 'a', label: 'A' }];
</script>

<Field label="Channel">
  {#if kind === 'Checkbox'}<Checkbox />
  {:else if kind === 'Input'}<Input />
  {:else if kind === 'Select'}<Select {options} />
  {:else if kind === 'Slider'}<Slider />
  {:else}<Textarea />{/if}
</Field>
