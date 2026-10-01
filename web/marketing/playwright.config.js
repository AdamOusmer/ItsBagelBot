// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { defineConfig } from '@playwright/test';

export default defineConfig({
    testDir: './tests',
    testIgnore: '**/compareVersion.spec.js',
    fullyParallel: true,
    workers: 3,
    retries: 1,
    use: {
        baseURL: 'http://localhost:4399',
    },
    // LOCALE_LAYOUT_BASE points the specs at a server that is already running, so none is started here.
    webServer: process.env.LOCALE_LAYOUT_BASE ? undefined : {
        command: 'bun --bun astro preview --port 4399 --ignore-lock',
        url: 'http://localhost:4399',
        reuseExistingServer: false,
        timeout: 60_000,
    },
});
