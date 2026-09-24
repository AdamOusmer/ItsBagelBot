// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { defineConfig } from '@playwright/test';

export default defineConfig({
    testDir: './tests',
    fullyParallel: true,
    workers: 3,
    retries: 1,
    use: {
        baseURL: 'http://localhost:4399',
    },
    webServer: {
        command: 'bun --bun astro preview --port 4399',
        url: 'http://localhost:4399',
        reuseExistingServer: false,
        timeout: 60_000,
    },
});
