// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

export type Payload = Record<string, unknown>;

export type Routes = Record<string, (payload: Payload) => unknown>;
