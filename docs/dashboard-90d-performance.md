# 90-day dashboard performance

Measured on 2026-10-03 against the local development PostgreSQL instance on the
low-performance ARM device. All database diagnostics were read-only. No indexes,
schema, configuration, or historical billing data were changed.

## Findings and results

The API-key hourly projection contains about 93,000 rows; the selected 90-day
range covers about 77,400 of them and 3,578 upstream credentials. An hourly
projection can still have many rows because its key includes the API key, user,
credential, group, platform, and model. A sequential scan over this small
projection for most of its history is reasonable; this is not a usage-log scan.

The original endpoint sequentially loaded model/user buckets, all API-key hourly
buckets, and credential/group distributions. The frontend then mounted all
3,578 credential rows even though its viewport showed only 15.

| Measurement | Before | After |
| --- | ---: | ---: |
| Uncached 90-day trend service, warm PostgreSQL pages | 1.79 s | 0.57–0.63 s |
| API-key query including transfer/decoding | 1.04 s | 0.47 s |
| API-key rows transferred for day granularity | 9,934 | 536 |
| Credential rows mounted in the browser at the top | 3,578 | 29 |

The pre-change live endpoint measured 1.86 s without an application cache hit
and 20 ms with one. After-change service timings exercise the modified code
directly against the same database, not an already-restarted HTTP process.

## Implementation

- Aggregate API keys by ID/hour before joining names. Select the global Top12
  in PostgreSQL, then coalesce only those keys into caller-local day buckets.
  Numeric billing sums stay numeric until the final result conversion.
- Run the three independent projection reads concurrently. In-process
  singleflight shares identical requests; cancelled callers stop waiting
  without aborting another caller's shared query. Shared work has a 30-second
  deadline. The existing distributed cache lock remains in place.
- Keep cache keys stable for equivalent hourly bucket ranges. The unchanged
  15-second TTL controls freshness; crossing a wall-clock 15-second boundary
  no longer causes a premature miss. Scope, timezone, range, and granularity
  remain part of the key, and cache payload version advances to v5.
- Opt the dashboard distribution table into windowed rendering. Measure actual
  row/header/viewport sizes, preserve full scroll height with spacer rows, keep
  original ranks and accessible row counts, and reset scrolling when switching
  dimension. Other compact tables retain their existing rendering behavior.

## Verification

The opt-in `TestLocalDashboardAPIKeyEquivalence` compares the optimized query
against the former all-key/hour aggregation. Requests, tokens, billing totals,
and Top12 membership match for unfiltered and user-filtered queries in Shanghai,
New York, and Kathmandu, with both hour and day granularity.

Frontend regression tests cover thousands of rows, deep scrolling, the last row,
shrinking result sets, and tab resets. A temporary browser fixture using the real
component and styles confirmed 29 mounted rows, a 16 px row height, and access to
the final credential. The fixture and its local server were removed afterward.

Run read-only database measurements from `backend/`:

```powershell
$env:AIRGATE_DASHBOARD_PERF_CONFIG = (Resolve-Path ../.dev/config.yaml).Path
go test -vet=off ./internal/infra/store -run '^TestLocalDashboard(APIKeyEquivalence|Performance)$' -count=1 -v
```

Optionally set `AIRGATE_DASHBOARD_PERF_HTTP=1` to measure the existing backend at
`127.0.0.1:9517` using a local test identity. Credentials and individual records
are never logged. Build, lint, and screenshots were intentionally not run.
