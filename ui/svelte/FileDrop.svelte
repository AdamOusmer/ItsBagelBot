<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import type { SvelteHTMLElements } from 'svelte/elements';
  import '../styles/elements/file-drop.css';

  type Own = {
    label: string;
    file?: File | null;
    accept?: string;
    onFileChange?: (file: File | undefined) => void;
    onDragChange?: (over: boolean) => void;
    class?: string;
  };

  let {
    label,
    file = null,
    accept,
    onFileChange,
    onDragChange,
    class: className = '',
    ...rest
  }: Own & Omit<SvelteHTMLElements['input'], keyof Own> = $props();

  let over = $state(false);

  const classes = $derived(['bb-file-drop', className || null].filter(Boolean).join(' '));

  function drag(next: boolean) {
    if (over === next) return;
    over = next;
    onDragChange?.(next);
  }

  function ondragover(event: DragEvent) {
    event.preventDefault();
    drag(true);
  }

  function ondrop(event: DragEvent) {
    event.preventDefault();
    drag(false);
    onFileChange?.(event.dataTransfer?.files?.[0]);
  }
</script>

<label class={classes} data-over={over ? '' : undefined} data-filled={file ? '' : undefined}
  ><input
    type="file"
    class="bb-file-drop__input"
    {accept}
    onchange={(event) => onFileChange?.(event.currentTarget.files?.[0])}
    {ondragover}
    ondragleave={() => drag(false)}
    {ondrop}
    {...rest}
  /><span class="bb-file-drop__text">{file ? file.name : label}</span></label
>
