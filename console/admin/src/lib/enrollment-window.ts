// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// The enrollment windows the overview offers, in a module both sides can
// import: the load parses `?days=` against this list, and the panel builds its
// segmented control from the same one. In $lib rather than in +page.server.ts
// because a component may not import a server module, and a second hand-kept
// literal in the component is how the control ends up offering a window the
// loader then rejects.
export const ENROLLMENT_WINDOWS = [7, 30, 90] as const;

export type EnrollmentWindow = (typeof ENROLLMENT_WINDOWS)[number];

export const DEFAULT_ENROLLMENT_WINDOW: EnrollmentWindow = 30;

/** The window named by `raw`, or the default when it names none. */
export function parseEnrollmentWindow(raw: string | null): EnrollmentWindow {
  const n = Number(raw);
  return (ENROLLMENT_WINDOWS as readonly number[]).includes(n)
    ? (n as EnrollmentWindow)
    : DEFAULT_ENROLLMENT_WINDOW;
}
