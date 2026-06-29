import { defineConfig } from '@playwright/test';

// Serves the already-built static output in dist/ on :4321 for the suite.
// `reuseExistingServer` lets a manually-running preview be reused locally.
export default defineConfig({
    testDir: './tests',
    fullyParallel: true,
    use: {
        baseURL: 'http://localhost:4399',
    },
    webServer: {
        command: 'bun ./node_modules/astro/bin/astro.mjs preview --port 4399',
        url: 'http://localhost:4399',
        reuseExistingServer: false,
        timeout: 60_000,
    },
});
