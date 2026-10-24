// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { Session } from './session';

export const isOwnerSession = (s: Session | null | undefined): s is Session =>
  !!s && !s.delegate_of && !s.impersonator_id;
