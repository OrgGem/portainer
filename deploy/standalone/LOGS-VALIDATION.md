# Logs validation receipt — 2026-11-18

Validated source at `316d60073` with the approved authentication and user
activity log changes. No commit, push, deployment, or existing service changes
were made.

## Focused checks

| Check | Result |
| --- | --- |
| Go: `./api/authlog ./api/http/handler/auth ./api/http/handler/useractivity ./api/oauth` | Passed with Go 1.26.10 in a disposable Docker container using `-p 2 -mod=readonly` |
| Auth and storage coverage | Restart/read, 30-day pruning, concurrent append, snapshot append isolation, deferred prune, corrupt/torn rows, canceled context, malformed and rate-limited requests, trusted-proxy IP, fail-open storage, Internal/LDAP/OAuth success and failure, and credential-free records passed |
| User activity API coverage | Global-admin access on all four routes; half-open time filters and pagination; safe activity projection; corrupt-source rejection before CSV headers; filtered, sorted CSV with 2,200 matches and page offset 2,000 passed |
| CSV format coverage | UTF-8 BOM, RFC 4180 CRLF, formula escaping, matching sort/filter behavior, and all-results export passed |
| Frontend Vitest | 6 files, 16 tests passed; covers local date bounds, table page-size cap and hidden “All”, export filters without paging, CSV blob error display, request cancellation, and export cancellation on unmount |
| TypeScript | `pnpm typecheck` passed |
| Changed-file ESLint | Passed with 3 warnings in `Datatable.tsx` (empty callback defaults and React Compiler/TanStack compatibility) |
| Production frontend build | `pnpm build --config webpack/webpack.production.js` compiled successfully; webpack reported 2 asset-size warnings |
| Whitespace | `git diff --check` passed; Git printed working-copy LF-to-CRLF notices |

Go ran in an isolated container. The host had Node 24.21.0 / PNPM 10.26.2 while
the package requests Node 22.22.1; the commands completed. The frontend build
also printed a missing optional `.env`, stale Browserslist data, and Babel
plugin deprecation notices.

## Database scope check

The source diff contains no changes under `api/database`, `api/datastore`, or
`api/portainer.go`. The changed log implementation has no schema, migration,
bucket, or database-version mutation references. This is a source-level check,
not runtime proof of database invariance.

## Scope limits

The production webpack build validates compilation and bundle generation; no
browser or new-UI runtime test was performed here. The smoke image reused the
base image's existing `/public` assets, so the runtime checks cover the current
Go API only. Activity projection was queried against an empty fixture; populated
configuration/deployment/metadata projection was covered by handler tests, not
runtime data. Runtime database buckets/schema were not inspected; database
invariance was checked in source only. The full repository test suite was not
run.

## Isolated runtime API smoke

The current-source Go binary ran in the uniquely tagged
`portainer-logs-smoke:20261118-526d7544` derivative image, based on the local
Portainer 2.45.3 image. The temporary container exposed HTTPS only on an
ephemeral `127.0.0.1` port and used its own named data volume. No existing
Portainer service was changed. The smoke container, image, volume, and private
probe artifacts were removed after the checks.

| Probe | Result |
| --- | --- |
| Admin initialization and login | HTTP 200 each |
| Authentication log JSON / CSV as global admin | HTTP 200; source available; CSV had UTF-8 BOM and CRLF |
| Authentication log JSON / CSV as anonymous | HTTP 401 |
| Authentication log JSON / CSV as standard user | HTTP 403 for both routes |
| Failed internal login | HTTP 422; failure row appeared in the admin log response |
| Authenticated logout | HTTP 204 with empty body after sending the auth cookie with its bearer token; logout row appeared |
| Restart persistence | After restarting only the smoke container, health/login/log reads returned HTTP 200; prior failure and logout rows remained (8 rows before restart, 9 after a new login) |
| Activity log JSON / CSV | HTTP 200, source available, zero rows in the empty fixture; CSV had UTF-8 BOM and CRLF |
| Known credential/token leakage scan | One persisted JSONL file, 9 rows; no synthetic admin/standard-user password, wrong-password probe, or login JWT was present |

The first health request returned HTTP 303 because the temporary, uninitialized
instance had passed Portainer's administrator setup deadline. Restarting that
owned container refreshed the deadline; admin initialization then succeeded.
This was bootstrap behavior, not a log endpoint failure.
