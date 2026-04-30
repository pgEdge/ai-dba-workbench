# E2E Tests — AI DBA Workbench

End-to-end tests for User Management, Token Management, and RBAC enforcement.

## Quick Start

### Prerequisites

- Docker and Docker Compose
- Node.js 22+
- `openssl` (for secret generation)

### Local Development

1. **Generate secrets (one-time):**
   ```bash
   bash scripts/setup-secrets.sh
   ```

2. **Start the containerized stack:**
   ```bash
   POSTGRES_PASSWORD=postgres \
   docker compose -f docker/docker-compose.yml up -d
   ```

3. **Bootstrap admin user:**
   ```bash
   docker compose -f docker/docker-compose.yml exec server \
     /usr/local/bin/ai-dba-server \
     -config /etc/pgedge/ai-dba-server.yaml -data-dir /data \
     -add-user -username admin -password "E2ETestPass123!" \
     -full-name "E2E Admin" -email "admin@e2e.test"

   docker compose -f docker/docker-compose.yml exec server \
     /usr/local/bin/ai-dba-server \
     -config /etc/pgedge/ai-dba-server.yaml -data-dir /data \
     -set-superuser -username admin
   ```

4. **Install dependencies:**
   ```bash
   npm ci && npx playwright install chromium
   ```

5. **Run tests:**
   ```bash
   E2E_ADMIN_USER=admin E2E_ADMIN_PASS="E2ETestPass123!" npm test
   ```

6. **View results:**
   ```bash
   npx playwright show-report
   ```

### Tear Down

```bash
docker compose -f docker/docker-compose.yml down -v
```

## Test Specific PostgreSQL Version

```bash
POSTGRES_IMAGE=postgres:16-alpine \
PGDATA_DIR=/var/lib/postgresql/data \
POSTGRES_PASSWORD=postgres \
docker compose -f docker/docker-compose.yml up -d
```

Supported versions:
- **16** — `postgres:16-alpine` with `/var/lib/postgresql/data`
- **17** — `postgres:17-alpine` with `/var/lib/postgresql/data`
- **18** — `ghcr.io/pgedge/pgedge-postgres:18-spock5-standard` with `/var/lib/pgsql/18/data`

## File Structure

```
client/tests/e2e/
├── README.md                    # This file
├── package.json                 # NPM dependencies
├── playwright.config.ts         # Playwright configuration
├── tsconfig.json               # TypeScript configuration
├── .gitignore                  # Git ignore rules
│
├── docker/
│   └── docker-compose.yml      # Containerized stack
│
├── config/
│   ├── ai-dba-server.yaml      # Server config (no LLM)
│   ├── ai-dba-collector.yaml   # Collector config
│   └── ai-dba-alerter.yaml     # Alerter config
│
├── secret/                     # Generated at runtime
│   ├── ai-dba.secret           # HMAC secret
│   └── pg-password             # PostgreSQL password
│
├── scripts/
│   └── setup-secrets.sh        # Generate secrets
│
├── helpers/
│   ├── api.helper.ts           # Fetch-based API wrapper
│   ├── auth.helper.ts          # Auth helpers
│   └── browser.helper.ts       # Playwright page utilities
│
├── fixtures/
│   ├── test-data.ts            # Test constants
│   ├── global.setup.ts         # Global setup (health check, admin login)
│   └── global.teardown.ts      # Global teardown (cleanup test data)
│
└── specs/
    ├── user-management.spec.ts # User CRUD tests
    ├── token-management.spec.ts # Token lifecycle tests
    └── rbac.spec.ts            # RBAC enforcement tests
```

## NPM Scripts

- `npm test` — Run all tests
- `npm run test:headed` — Run tests with browser visible
- `npm run test:debug` — Run tests in debug mode
- `npm run test:ui` — Run tests with interactive UI
- `npm run report` — Open HTML test report

## Environment Variables

| Variable | Default | Purpose |
|---|---|---|
| `E2E_BASE_URL` | `http://localhost:3000` | Client base URL |
| `E2E_API_URL` | `http://localhost:8080` | Server API URL |
| `E2E_ADMIN_USER` | `admin` | Admin username for setup/teardown |
| `E2E_ADMIN_PASS` | `E2ETestPass123!` | Admin password for setup/teardown |
| `CI` | `false` | Set to `true` in CI environments |

## Test Coverage

### User Management (7 tests)
- ✅ Create user via API
- ✅ Create user via UI
- ✅ Update user via API
- ✅ Update user via UI
- ✅ Delete user via API
- ✅ Delete user via UI
- ✅ Permission enforcement (403 without `manage_users`)

### Token Management (7 tests)
- ✅ Create token
- ✅ List tokens
- ✅ Bearer token authentication
- ✅ Set token scope
- ✅ Clear token scope
- ✅ Revoke token
- ✅ Token UI creation

### RBAC Enforcement (7 tests)
- ✅ Unauthenticated request → 401
- ✅ Invalid cookie → 401
- ✅ Token scope blocks out-of-scope connections
- ✅ User without `manage_users` → 403
- ✅ Superuser bypasses restrictions
- ✅ Cookie invalidated after logout
- ✅ Revoked bearer token → 401

## Architecture

### Services (Docker Compose)

- **postgres** — Database (PG 16/17/18, health checked)
- **server** — REST API on port 8080
- **collector** — Background worker for metrics
- **alerter** — Background worker for anomalies
- **client** — React SPA on port 3000 (nginx)

All services connect via `e2e-network` bridge.

### Test Infrastructure

- **API Helper** (`helpers/api.helper.ts`) — fetch-based wrapper with manual cookie/Bearer token handling
- **Auth Helper** (`helpers/auth.helper.ts`) — Login, token creation, cleanup utilities
- **Browser Helper** (`helpers/browser.helper.ts`) — Playwright page helpers using ARIA labels
- **Global Setup** (`fixtures/global.setup.ts`) — Health check, admin login, storage state save
- **Global Teardown** (`fixtures/global.teardown.ts`) — Cleanup test data (`e2e-test-*` prefix)

### Key Design Decisions

- **API tests** call `http://localhost:8080` directly (bypass nginx)
- **Browser tests** use `http://localhost:3000` (nginx proxy to server)
- **Test data cleanup** uses `e2e-test-*` naming prefix
- **No real LLM** — LLM is disabled in `config/ai-dba-server.yaml`
- **Manual cookie handling** — Node fetch doesn't auto-manage cookies
- **Playwright storage state** — Tests skip UI login by injecting session cookie

## GitHub Actions

The `.github/workflows/e2e-tests.yml` workflow:

- Triggers on push/PR to `main` and `develop`
- Runs matrix across PG 16, 17, 18
- Builds Docker images, starts stack, bootstraps admin, runs tests
- Uploads artifacts (HTML report, JUnit XML, traces on failure)
- Cleans up with `docker compose down -v`

## Troubleshooting

### Tests time out waiting for server

Check server logs:
```bash
docker compose -f docker/docker-compose.yml logs server
```

### PostgreSQL won't start

Check PG logs:
```bash
docker compose -f docker/docker-compose.yml logs postgres
```

### Secrets not generated

Run:
```bash
bash scripts/setup-secrets.sh
```

### Port already in use

Change port mappings in `docker/docker-compose.yml`:
```yaml
ports:
  - "9999:80"  # client (was 3000)
  - "9998:8080"  # server (was 8080)
  - "9997:5432"  # postgres (was 5432)
```

Then update `E2E_BASE_URL` and `E2E_API_URL` env vars.

## Contributing

When adding new E2E tests:

1. Use `makeTestUsername(suffix)` from `test-data.ts` for user naming
2. Prefix token annotations with `e2e-test-` for cleanup
3. Use `aria-label` and role selectors (no `data-testid`)
4. Keep tests independent (can run in any order)
5. Use `test.use({ storageState: '.auth/admin.json' })` for browser auth

## References

- [Playwright Testing Guide](https://playwright.dev/)
- [Server API Endpoints](../../docs/admin-guide/api/reference.md)
- [RBAC Architecture](../../docs/admin-guide/rbac.md)
