package builtin

// DefaultCommand is a bot-shipped command. Definitions live in code; a
// broadcaster may disable one or override its response through the reserved
// modules-service row command.<name> (see CommandModule), but cannot change the
// command's gate semantics.
type DefaultCommand struct {
	Name             string
	Response         string
	Perm             string // module.ParsePerm vocabulary; "" = everyone
	StreamOnlineOnly bool
	Cooldown         uint // seconds; 0 = none
}

// defaultCommandPrefix is the reserved modules-service name namespace under which
// a broadcaster's per-command override (enabled + response) is stored, e.g.
// "command.bot". Absence of a row means the shipped default applies, enabled.
const defaultCommandPrefix = "command."

// defaultCommands is the built-in command table, keyed by lower-case name. Add
// shipped commands here; each is automatically toggleable/overridable per user.
var defaultCommands = map[string]DefaultCommand{
	"bot": {
		Name:     "bot",
		Response: "I am ItsBagelBot 🥯 — github.com/AdamOusmer/ItsBagelBot",
		Perm:     "everyone",
	},
}

// lookupDefault returns the shipped command for a lower-cased name.
func lookupDefault(name string) (DefaultCommand, bool) {
	cmd, ok := defaultCommands[name]
	return cmd, ok
}
