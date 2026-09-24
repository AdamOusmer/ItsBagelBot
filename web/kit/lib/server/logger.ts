// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// No worker transport: pino transports break in the distroless bundle.
import pino from 'pino';

export const logger = pino({
  level: process.env.LOG_LEVEL ?? 'info'
});
