// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

export const ENROLLMENT_WINDOWS = [7, 30, 90] as const;

export type EnrollmentWindow = (typeof ENROLLMENT_WINDOWS)[number];

export const DEFAULT_ENROLLMENT_WINDOW: EnrollmentWindow = 30;

export function parseEnrollmentWindow(raw: string | null): EnrollmentWindow {
  const n = Number(raw);
  return (ENROLLMENT_WINDOWS as readonly number[]).includes(n)
    ? (n as EnrollmentWindow)
    : DEFAULT_ENROLLMENT_WINDOW;
}
