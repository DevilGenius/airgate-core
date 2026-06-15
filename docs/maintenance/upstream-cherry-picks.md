# Upstream Cherry-Pick Tracking

This file tracks upstream commits that are cherry-picked, rewritten, partially
absorbed, deferred, or skipped on this repository's local integration branch.

Current local branch: `feature`

Last reviewed: 2026-06-14

## Status Legend

| Status | Meaning |
| --- | --- |
| `pending` | Should be absorbed, but no local rewrite has been committed yet. |
| `pending-partial` | Only part of the upstream patch should be absorbed. |
| `pending-group` | Must be absorbed together with related upstream commits. |
| `deferred` | Useful, but not urgent or too broad for the current batch. |
| `skipped` | Intentionally not absorbing as-is. |
| `superseded` | Local code already has a better or equivalent implementation. |
| `rewritten` | Logic has been absorbed into local commit(s) with different hashes. |

## airgate-core

### Recommended Next

| Upstream commit | Local commit | Status | Notes |
| --- | --- | --- | --- |
| `68e7658` | - | `pending` | Recognize Claude cache creation input usage; small billing correctness fix. |
| `ef39149` | - | `pending` | Record partial image usage on stream abort; prevents usage leakage on aborted streams. |
| `76d67b3` | - | `pending-partial` | Absorb plugin upload/proxy/download size limits and non-leaking plugin handler errors. Skip `plugin/request.go` body limit because local `feature` already has more specific 10MB/32MB gateway limits. |
| `d967c35` | - | `pending-group` | Introduces CORS and auth endpoint IP rate limiting. Must be rewritten with `963326f`, not applied alone. |
| `963326f` | - | `pending-group` | Fixes the IP rate limiter from `d967c35`: atomic `lastSeen` and shutdown cleanup. |
| `5304dde` | - | `pending` | Use real user balance plus API key quota for `/v1/usage` cc-switch checks. |
| `dc4298c` | - | `pending` | Align Codex config/auth template on the user API key page. Low risk. |

### Second Batch

| Upstream commit | Local commit | Status | Notes |
| --- | --- | --- | --- |
| `105914c` | - | `deferred` | Reduce startup migration WAL churn; useful but touches startup/deploy behavior. |
| `0635ed3` | - | `deferred` | Adds `users.update_balance` host method. Useful if plugin idempotent balance updates are needed; touches ent/app/plugin/server. |
| `b794d32` | - | `deferred` | Query key centralization and i18n cleanup. High frontend overlap with local changes. |

### Requires Separate Planning

| Upstream commit | Local commit | Status | Notes |
| --- | --- | --- | --- |
| `349c7f6` | - | `deferred` | Account capability routing. High overlap with local scheduler/account UI changes. |
| `8c2bd53` | - | `deferred` | Plugin metadata capability query refactor. Should be handled with the broader metadata declaration work. |
| `1d790a8` | - | `deferred` | Metadata-driven plugin/core hardcode removal. Must be planned as a group. |
| `0c8e517` | - | `deferred` | Metadata-driven error format selection. Must be planned with plugin metadata changes. |
| `c5de1e6` | - | `deferred` | Declarative model scheduling/status page hardcode removal. Needs matching plugin metadata support. |

### High Conflict / Do Not Apply As-Is

| Upstream commit | Local commit | Status | Notes |
| --- | --- | --- | --- |
| `1b3f34c` | - | `deferred` | Image usage billing controls. Overlaps heavily with local sell-rate, group-rate, and image pricing work. Extract logic only after dedicated comparison. |
| `8efe984` | - | `deferred` | Pending core updates tied to image/billing changes; evaluate with `1b3f34c`. |
| `e7f5f39` | - | `skipped` | Removes per-message account lock and changes all-routes capacity failure semantics. Conflicts with local 429/monitoring behavior; do not apply as-is. |
| `9d5aca8` | - | `deferred` | Handler/service refactor. Broad structural change; not a targeted reliability fix. |
| `86c0186` | - | `deferred` | `normalizePage` / `parseID` dedup refactor. Broad overlap with local app/handler changes. |

### Documentation / Tooling

| Upstream commit | Local commit | Status | Notes |
| --- | --- | --- | --- |
| `6009894` | - | `deferred` | Commit hook / CLAUDE.md / playground tooling. Not needed for runtime reliability. |
| `a76f358` | - | `deferred` | CLAUDE.md terminology. |
| `34b0668` | - | `deferred` | CLAUDE.md cleanup. |
| `0423355` | - | `deferred` | Architecture docs cleanup. |
