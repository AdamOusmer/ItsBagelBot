// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

/**
 * Registry: name -> component, resolved from a directory listing rather than a
 * hand-kept table, so adding a screen or a widget is one new file.
 *
 * The `import.meta.glob` call itself has to stay at each call site: Vite
 * rewrites it at build time and needs a literal pattern, so a `globDir('./*')`
 * that took the pattern as an argument would compile to an empty record. What
 * is shared is everything after it, which is the part that was written twice
 * and would have gone on being written a third time for the next registry.
 */
export function globDir<Name extends string>(
    modules: Record<string, unknown>,
): Record<Name, unknown> {
    return Object.fromEntries(
        Object.entries(modules).map(([path, component]) => [
            path.slice('./'.length, -'.astro'.length),
            component,
        ]),
    ) as Record<Name, unknown>;
}
