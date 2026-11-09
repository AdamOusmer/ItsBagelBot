# Shared service startup

`Platform.Startup` assembles a one-for-one supervisor from explicitly declared
base and enabled-service child builders. Services keep their own child order,
configuration, identity checks, and domain behavior. The enabled install hook
runs before child construction; any failed install, construction, or supervisor
start invokes the uninstall hook once. Cleanup errors propagate to the caller.

`prep_stop/3` runs a service's drain hook only when enabled. `stop/1` runs its
uninstall hook. These helpers do not manage application dependencies or perform
backend operations.

Run `mix format --check-formatted` and `mix test` in this directory.
