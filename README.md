# DriftGuard

> Keep `.env.example` honest. Zero-config env-variable drift detection for polyglot codebases.

DriftGuard statically scans your code for the environment variables it actually reads
— `os.Getenv`, `process.env.X`, `Deno.env.get`, `getenv()/$_ENV/env()`, `os.environ`
— and compares that against your `.env.example`. It catches the two failures that bite
in CI and production:

- **Missing** — a key the code reads but nobody documented. This is the one that crashes
  staging/production on a value that was never provisioned.
- **Stale** — a key documented in `.env.example` that nothing reads anymore.

It also has a CI mode that **seeds a placeholder `.env`** when one is missing, so a strict
loader (`node --env-file=.env`, etc.) doesn't abort the build before your tests run.

No config, no schema to maintain — the schema *is* your code.

## Install

```sh
go install github.com/muhammetsafak/driftguard/cmd/driftguard@latest
```

## Usage

```sh
# Audit drift against .env.example (exit 1 if a used key is undocumented)
driftguard check
driftguard check ./services/api          # a specific directory
driftguard check --example .env.sample   # a different example file
driftguard check --strict-stale          # also fail on documented-but-unused keys

# CI helper: if .env is missing, write a placeholder one from discovered keys
driftguard seed                          # only acts when GITHUB_ACTIONS / CI is set
driftguard seed --force                  # act anywhere
```

Example output:

```
Scanned . — 7 env key(s) referenced in code.

Missing from .env.example (used in code, not documented):
  + STRIPE_WEBHOOK_SECRET          api/billing/webhook.go:42
  + REDIS_TLS                      api/cache/redis.go:18

Stale in .env.example (documented, never used):
  - LEGACY_SMTP_HOST
```

### In GitHub Actions

```yaml
- run: go install github.com/muhammetsafak/driftguard/cmd/driftguard@latest
- run: driftguard seed     # unblock strict --env-file loaders before the build
- run: driftguard check    # fail the job on undocumented keys
```

## What it scans

| Language        | Extensions                         | Idioms |
| --------------- | ---------------------------------- | ------ |
| Go              | `.go`                              | `os.Getenv`, `os.LookupEnv` |
| JS / TS / Deno  | `.js .jsx .ts .tsx .mjs .cjs`      | `process.env.X`, `process.env['X']`, `Deno.env.get('X')` |
| PHP             | `.php`                             | `getenv()`, `$_ENV[]`, `$_SERVER[]`, `env()` |
| Python          | `.py`                              | `os.environ[]`, `os.environ.get()`, `os.getenv()` |

Vendored / generated directories (`node_modules`, `vendor`, `dist`, `.venv`, …) are
skipped automatically.

## Exit codes

| Code | Meaning |
| ---- | ------- |
| `0`  | no drift (or `--allow-missing`) |
| `1`  | drift found (missing keys; or stale with `--strict-stale`) |
| `2`  | usage / IO error |

## Scope & limits (honest list)

- **Precision over recall.** It extracts **literal, statically-knowable** keys only.
  A computed name (`process.env['PREFIX_' + name]`) is invisible — by design, so the
  tool never guesses.
- **Regex, not AST (for now).** It matches the canonical idioms above; an exotic wrapper
  around env access won't be seen. AST-backed scanning is on the roadmap.
- **Placeholder values are empty**, never fabricated secrets — present enough to satisfy
  a strict loader, never something mistaken for a real credential.

## License

MIT
