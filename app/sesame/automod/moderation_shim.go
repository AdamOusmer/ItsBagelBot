package automod

import "ItsBagelBot/internal/moderation"

// Thin re-exports of the shared content primitives (internal/moderation), kept
// so the automod public surface and its callers (main's lexicon reloader, the
// tests) read naturally without importing two packages.

// Lexicon is the compiled categorized word-list artifact.
type Lexicon = moderation.Lexicon

// Normalize folds a message into its detection skeleton (see moderation).
func Normalize(dst []byte, text string) []byte { return moderation.Normalize(dst, text) }

// EmbeddedLexicon returns the compiled starter artifact shipped in the binary.
func EmbeddedLexicon() *Lexicon { return moderation.EmbeddedLexicon() }

// LoadLexiconDir compiles a lexicon from override files in dir, falling back to
// the embedded starter per missing file.
func LoadLexiconDir(dir string) (*Lexicon, error) { return moderation.LoadLexiconDir(dir) }
