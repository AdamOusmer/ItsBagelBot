<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import '../styles/elements/file-drop.css';

  let {
    label,
    file = null,
    accept,
    onfile,
    ondragchange,
    class: className = '',
    ...rest
  }: {
    label: string;
    file?: File | null;
    accept?: string;
    onfile?: (file: File | undefined) => void;
    ondragchange?: (over: boolean) => void;
    class?: string;
    [key: string]: unknown;
  } = $props();

  let over = $state(false);

  const classes = $derived(['bb-file-drop', className || null].filter(Boolean).join(' '));

  function drag(next: boolean) {
    if (over === next) return;
    over = next;
    ondragchange?.(next);
  }

  function ondragover(event: DragEvent) {
    event.preventDefault();
    drag(true);
  }

  function ondrop(event: DragEvent) {
    event.preventDefault();
    drag(false);
    onfile?.(event.dataTransfer?.files?.[0]);
  }
</script>

<label class={classes} data-over={over ? '' : undefined} data-filled={file ? '' : undefined}
  ><input
    type="file"
    class="bb-file-drop__input"
    {accept}
    onchange={(event) => onfile?.(event.currentTarget.files?.[0])}
    {ondragover}
    ondragleave={() => drag(false)}
    {ondrop}
    {...rest}
  /><span class="bb-file-drop__text">{file ? file.name : label}</span></label
>
