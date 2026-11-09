# Shared vendor boundary check

`Platform.Boundary` is a test-only package. `violations/1` parses each given
Elixir file and returns `{path, reference}` for every direct use of a vendor
library that a shared package owns: Gnat, Mint, Redix, New Relic, Protox,
Cowboy, Cowlib, Ranch, WebSock, Plug.Cowboy, Torque, Jason, Poison, Jiffy,
Thoas, Elixir's built-in `JSON` and direct `:torque`, `:json`, `:jiffy` and
`:thoas` calls (JSON goes through `Platform.Sift`). Files that begin with
`# Code generated` are skipped. Connection names such as `:gnat` stay allowed;
only calls into those modules count.

Services add it with `only: :test` and assert that their `lib`, `config` and
`test` sources report no violations. Run `mix format --check-formatted` and
`mix test` in this directory.
