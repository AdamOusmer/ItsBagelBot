// The .test.ts files next to the server modules import from 'bun:test', which
// is declared by bun-types (pulled in by @types/bun). Automatic @types
// inclusion does not reach it from this workspace -- @types/bun sits in the
// console root's node_modules, not the dashboard's -- so svelte-check reported
// "Cannot find module 'bun:test'" on every run. Referencing it explicitly from
// a .d.ts inside src/ puts it in the program regardless of where the package
// was hoisted.
/// <reference types="bun" />
