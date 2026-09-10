// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// The adapters import their contract stylesheet as a side effect
// (`import '../styles/elements/cursor.css'`), which is how every bundler that
// consumes this package is told to emit the CSS. tsc and svelte-check both see
// a module that does not exist and report TS2307; there is no compiler option
// for it, because the module genuinely only exists after a bundler makes it.
declare module '*.css';
