# govisualize

Explore public GitHub Go modules as interactive call trees. Expand calls, inspect source, and follow struct creations to their definitions. Analysis reads source without executing repository code or downloading its dependencies.

Requires a desktop browser and a public repository containing a Go module with a `main` function.

## Run with Docker

Requires Docker with Compose v2 and internet access. From the repository root:

```sh
docker compose up --build
```

Open http://localhost:3000, enter a repository URL, and choose a module if prompted. No account, token, or environment file is required.

Select a function to inspect its source, a connection to highlight its call site, or a creation to view its definition and initialization.

Stop with **Ctrl+C**. To remove the containers:

```sh
docker compose down
```

If port 3000 is in use, set `GOVISUALIZE_PORT=3001` in a root `.env` file and restart Compose.

## Limitations

- Each submission analyzes the current default branch at a fixed commit. Reopening a shared link resolves the default branch again.
- External calls, interface dispatch, function-variable calls, anonymous function bodies, and implicit initialization are not modeled. Unsupported calls appear in analysis notes.
- Test files and nested modules are excluded. Creations cover named module structs; collections and implicit zero values are not modeled. Source order does not imply execution order.
