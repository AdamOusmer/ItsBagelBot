// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Public surface of the Fossabot config-import source. Layout mirrors
// nightbot/: ./fetch pulls the channel's public commands directory over the
// cached API with nothing but a channel name, ./envelope decides what a feed
// is, ./variables translates its variable syntax, ./parse turns rows into the
// canonical manifest.

export { detectFossabot, FossabotExportError } from './envelope';
export { FB_CODE, parseFossabot } from './parse';
export { translateVariables } from './variables';
export { fetchFossabot, FossabotFetchError, DEFAULT_API_BASE, FETCH_TIMEOUT_MS } from './fetch';
