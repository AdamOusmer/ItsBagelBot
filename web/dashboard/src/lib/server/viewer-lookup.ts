// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { resolveViewerId } from './loyalty-store';
import { createViewerResolver } from './viewer-resolver';

export const resolveCurrentViewer = createViewerResolver({ lookup: resolveViewerId });
