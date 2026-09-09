// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Public surface of the Wizebot config-import source. Layout mirrors
// nightbot/: ./fetch replays the streaming website's own three-call session to
// reach the published command list, ./envelope decides what that list is,
// ./entities and ./tags translate the text it carries, ./parse turns rows into
// the canonical manifest.
//
// Server-side only: fetchWizebot makes cross-origin calls with a session
// cookie, which no browser bundle may do, so the import page reaches this
// module through the server strategy registry and never imports it.

export { decodeEntities } from './entities';
export { detectWizebot, WizebotExportError } from './envelope';
export { translateTags } from './tags';
export { parseWizebot, WB_CODE } from './parse';
export { fetchWizebot, WizebotFetchError, HANDLE_SHAPE, FETCH_TIMEOUT_MS, siteUrlFor } from './fetch';
