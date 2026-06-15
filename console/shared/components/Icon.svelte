<script lang="ts">
  import { icons, type IconName } from '../lib/icons';
  let { name, size = 18, fill = 'none' }: { name: IconName; size?: number; fill?: string } = $props();
</script>

<svg viewBox="0 0 24 24" {fill} width={size} height={size} aria-hidden="true">
  <!-- icons map holds only static design constants -->
  {@html icons[name]}
</svg>
