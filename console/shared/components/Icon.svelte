<script lang="ts">
  import { icons, type IconName } from '../lib/icons';
  let { name, size = 18, fill = 'none' }: { name: IconName; size?: number; fill?: string } = $props();
</script>

<!-- Self-sufficient rendering: stroke/width/caps live on the svg itself, so an
     icon draws correctly wherever it's dropped — consumers may still override
     via CSS (their rules win over presentation attributes), but a spot without
     any svg styling no longer renders an invisible black-on-dark glyph. -->
<svg
  viewBox="0 0 24 24"
  {fill}
  width={size}
  height={size}
  stroke="currentColor"
  stroke-width="1.6"
  stroke-linecap="round"
  stroke-linejoin="round"
  aria-hidden="true"
>
  <!-- icons map holds only static design constants -->
  {@html icons[name]}
</svg>
