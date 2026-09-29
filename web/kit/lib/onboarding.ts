// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

export function clearOnboarding() {
  try {
    localStorage.removeItem('bb-onboarded');
  } catch {
    return;
  }
}
