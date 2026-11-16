<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  let { title, description, url, schema }: { title: string; description: string; url: string; schema?: Record<string, unknown> } = $props();

  const jsonLd = $derived(schema ? `<script type="application/ld+json">${JSON.stringify(schema).replaceAll('<', '\\u003c')}<\/script>` : '');
</script>

<svelte:head>
  <title>{title}</title>
  <meta name="description" content={description} />
  <link rel="canonical" href={url} />
  <meta property="og:url" content={url} />
  <meta property="og:title" content={title} />
  <meta property="og:description" content={description} />
  <meta name="twitter:title" content={title} />
  <meta name="twitter:description" content={description} />
  <!-- eslint-disable-next-line svelte/no-at-html-tags -->
  {@html jsonLd}
</svelte:head>
