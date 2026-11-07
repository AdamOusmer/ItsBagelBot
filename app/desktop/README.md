# ItsBagelBot desktop

A [Wails v3](https://v3.wails.io) app with a Svelte 5 frontend built on
`@bagel/ui`. It currently shows the sign-in screen, using the copy in
`locales/en/console/login.json`. Web links open in the system browser.

Its own Go module, so desktop and CGO dependencies stay out of server binaries.

## Commands

Install the CLI once:

```sh
go install github.com/wailsapp/wails/v3/cmd/wails3@v3.1.0
```

Run from this directory:

```sh
wails3 dev          # live reload window
wails3 build        # bin/ItsBagelBot
wails3 task darwin:package   # bin/ItsBagelBot.app
```

Frontend checks, from `frontend/`:

```sh
bun install
bun run check
```
