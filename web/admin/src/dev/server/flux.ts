// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { FluxTier, ImageOverride } from '#lib/deploys/types';
import type { PinRequest } from '#lib/server/flux';
import { demoFluxTiers } from '../fixtures';

export * from '#lib/server/flux';

export async function fluxTiers(): Promise<FluxTier[]> {
  return demoFluxTiers();
}

export async function reconcile(): Promise<void> {}

export async function setSuspended(): Promise<void> {}

export async function pinImage(req: PinRequest): Promise<ImageOverride> {
  return { name: `ghcr.io/demo/${req.service}`, newTag: req.tag, digest: 'sha256:demo' };
}

export async function clearImage(): Promise<void> {}
