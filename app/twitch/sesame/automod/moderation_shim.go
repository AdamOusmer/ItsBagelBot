// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package automod

import "ItsBagelBot/internal/moderation"

type Lexicon = moderation.Lexicon

func Normalize(dst []byte, text string) []byte { return moderation.Normalize(dst, text) }

func EmbeddedLexicon() *Lexicon { return moderation.EmbeddedLexicon() }

func LoadLexiconDir(dir string) (*Lexicon, error) { return moderation.LoadLexiconDir(dir) }
