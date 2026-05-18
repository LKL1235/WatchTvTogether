# AGENTS.md

## Cursor Cloud specific instructions

This workspace has two repos: **WatchTvTogether** (Go backend) and **WatchTvTogether-Web** (Vue 3 frontend).

### Backend (WatchTvTogether)

- **Tech**: Go 1.25, Gin, PostgreSQL 15, Redis 缓存（唯一后端）, Ably for realtime.
- **Run**: `POSTGRES_DSN="postgres://watchtogether:watchtogether@localhost:5432/watchtogether?sslmode=disable" ABLY_ROOT_KEY="devapp.devkey:devsecret" go run ./cmd/server` — listens on `:8080`.
- **Ably key is required** to start the server. Use a dummy key (`devapp.devkey:devsecret`) for local dev; realtime sync won't work but all other features function normally.
- **Schema note**: `internal/store/postgres/schema.sql` is missing the `email` column on `users`. Run `ALTER TABLE users ADD COLUMN IF NOT EXISTS email TEXT NOT NULL DEFAULT '';` after the app auto-creates the schema, or the `GetByEmail` queries will fail.
- **Lint**: `go vet ./...`
- **Build**: `go build ./...`
- **No Go tests** are present in the repo currently.

### Frontend (WatchTvTogether-Web)

- **Tech**: Vue 3 + Vite + TypeScript + Pinia.
- **Run**: `VITE_API_BASE="" npm run dev` — listens on `:5173`, proxies `/api` and `/static` to `localhost:8080`.
- **Critical**: Set `VITE_API_BASE=""` or the frontend will call the production server instead of the local backend.
- **Lint**: `npx vue-tsc --noEmit`
- **Test**: `npm run test` (vitest, 18 tests)
- **Build**: `npm run build`

### PostgreSQL setup

The update script installs PostgreSQL and creates the `watchtogether` user/database. After starting the backend once (which auto-creates tables), run the email column migration:
```sql
ALTER TABLE users ADD COLUMN IF NOT EXISTS email TEXT NOT NULL DEFAULT '';
CREATE UNIQUE INDEX IF NOT EXISTS idx_users_email ON users(email) WHERE email != '';
```

### Creating a test user

Registration requires email verification (Resend API). Without `RESEND_API_KEY`, insert a user directly:
```sql
INSERT INTO users (id, email, username, password_hash, nickname, avatar_url, role, created_at, updated_at)
VALUES ('test-user-001', 'testuser@example.com', 'testuser',
        '$2a$10$9ZfrgL973QR5AOV2fV3FsOrgqpGOwkQFCLDhnz8SXMcc8i1DiKfOa',
        'TestUser', '', 'user', NOW(), NOW());
```
Password: `testpass1`
