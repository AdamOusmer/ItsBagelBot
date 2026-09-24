// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

declare module '*.svelte' {
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  const component: any;
  export default component;
}

declare module '*.astro' {
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  const component: any;
  export default component;
}

declare module '*.css';
