# Monalias agent notes

## Project layout

- Go entrypoint: `cmd/monalias/main.go`
- Public HTTP handlers: `internal/http/public.go`
- Admin HTTP handlers: `internal/http/admin.go`
- GraphQL schema + resolvers: `internal/graphql`
- SQLite schema: `internal/db/schema.sql`
- Embedded UI: `internal/ui`
- Flutter source: `ui/`

## Conventions

- Keep config in `internal/config` and wire through `cmd/monalias/main.go`.
- Prefer strict, small handlers with clear HTTP status codes and JSON error bodies.
- Any DB change must update `internal/db/schema.sql` and, if relevant, `internal/db/queries.sql`.
- Avoid adding new runtime dependencies unless required.

## Local commands

- Run the server: `go run ./cmd/monalias`
- Build binary: `go build ./cmd/monalias`

## Notes

- The admin listener must stay private by default (`127.0.0.1:8080`).
- Public resolve requests are rate-limited per IP.
- The signing key file is required for startup.
