# ItsBagelBot Documentation

This directory contains the documentation for ItsBagelBot, built with [Astro Starlight](https://starlight.astro.build).

## 🚀 Project Structure

- `src/content/docs/`: Markdown and MDX files for the documentation routes.
  - `adr/`: Architecture Decision Records.
  - `architecture/`: General architecture documentation.
  - `infrastructure/`: Infrastructure setup and deployment docs.
  - `data-and-state/`: Data models and state management.
  - `microservices/`: Details of individual microservices.
  - `qa/`: QA Reports and testing docs.
  - `reference/`: API and system references.
- `src/assets/`: Images and other assets used in docs.
- `public/`: Static assets like favicons.
- `astro.config.mjs`: Starlight configuration (sidebar, theme, etc.).

## 🧞 Commands

Run these from the `docs/` directory using `bun`:

| Command                   | Action                                           |
| :------------------------ | :----------------------------------------------- |
| `bun install`             | Installs dependencies                            |
| `bun dev`                 | Starts local dev server at `localhost:4321`      |
| `bun build`               | Build your production site to `./dist/`          |
| `bun preview`             | Preview your build locally, before deploying     |
| `bun astro ...`           | Run CLI commands like `astro add`, `astro check` |

## 📝 Architecture Decision Records (ADRs)

ADRs are managed with [`adr-tools`](https://github.com/npryce/adr-tools) (install with `brew install adr-tools`) and live under `src/content/docs/adr/`.

Use the project wrapper so the local template is picked up:

```sh
./bin/adr new "Short title of the decision"
./bin/adr new -s 3 "Supersede decision 3"
./bin/adr list
```

## 👀 Writing Documentation

- The documentation uses Starlight's standard Markdown and MDX capabilities.
- We have integrated `astro-mermaid` for diagrams. See `astro.config.mjs` for custom theme configuration.
