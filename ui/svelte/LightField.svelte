<script lang="ts">
    // Copyright (c) 2026 Adam Ousmer. All rights reserved.
    // Proprietary. No license granted. See LICENSE.md.

    import { onMount } from 'svelte';
    import { field } from '../lib/light-field';
    import '../styles/elements/light-field.css';

    let { class: className = '', warmth = 0.7 }: { class?: string; warmth?: number } = $props();

    let canvas: HTMLCanvasElement;

    onMount(() => field(canvas, { warmth }) ?? undefined);
</script>

<canvas
    class={['bb-light-field', className].filter(Boolean).join(' ')}
    data-field
    data-warmth={warmth}
    aria-hidden="true"
    bind:this={canvas}
></canvas>
