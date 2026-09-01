// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Public surface of the Nightbot config-import parser. Layout mirrors moobot/:
// ./envelope decides what a saved export is, ./variables translates its
// variable syntax, ./parse turns rows into the canonical manifest.

export { detectNightbot, NightbotExportError } from './envelope';
export { NB_CODE, parseNightbot } from './parse';
export { translateVariables } from './variables';
