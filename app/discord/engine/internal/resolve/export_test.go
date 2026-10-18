// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package resolve

const MaxWarned = maxWarned

func (w *ConfigWarnings) First(guildID, field string) bool { return w.first(guildID, field) }
