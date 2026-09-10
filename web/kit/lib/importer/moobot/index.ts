// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Public surface of the Moobot config-import parser. Layout mirrors the Go
// original: ./parse is moobot.go, ./tags is tags.go.

export { detectMoobot, MOOBOT_SECTIONS, MoobotExportError, normalizeName, parseMoobot } from './parse';