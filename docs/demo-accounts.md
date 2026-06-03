# Demo Accounts

Demo accounts are created only by an explicit manual command. They are not created during normal startup, registration, migrations, Docker startup, or CI.

## Accounts

The command maintains these accounts:

- `demo-en`
- `demo-sr`
- `demo-ru`

The default password is intended for local/demo use only. Override it per account when needed:

- `DEMO_EN_PASSWORD`
- `DEMO_SR_PASSWORD`
- `DEMO_RU_PASSWORD`

## Refresh Demo Data

Refresh keeps existing demo users and workspaces, replaces their operational data, and writes the current demo dataset version marker.

```bash
DEMO_SEED_CONFIRM=seed-demo-accounts go run ./cmd/seed_demo_accounts
```

## Reset Demo Accounts

Reset deletes and recreates only the managed `demo-*` accounts and their owned records.

```bash
DEMO_SEED_CONFIRM=seed-demo-accounts DEMO_SEED_MODE=reset go run ./cmd/seed_demo_accounts
```

Use reset only when stable demo workspace IDs are not important. For routine updates after adding new feature fixtures, use refresh mode.

## Dataset Updates

Demo fixtures live under `internal/demo_seed`. When a new product feature needs demo data:

1. Add or update the fixture fields.
2. Bump `DatasetVersion`.
3. Run `go test ./internal/demo_seed -count=1`.
4. Run refresh mode against the target demo database.

Russian and Serbian fixture values are intentionally present only in this manual demo seeding path.
