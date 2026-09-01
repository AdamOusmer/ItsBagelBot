// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Public surface of the Nightbot config-import source. Layout mirrors moobot/
// plus a fetch layer: ./fetch pulls the account's config over the REST API
// with an OAuth token, ./envelope decides what a bundle is, ./variables
// translates its variable syntax, ./parse turns rows into the canonical
// manifest.

export { detectNightbot, NightbotExportError } from './envelope';
export { NB_CODE, parseNightbot } from './parse';
export { translateVariables } from './variables';
export { fetchNightbot, NightbotFetchError, DEFAULT_API_BASE, FETCH_TIMEOUT_MS } from './fetch';
