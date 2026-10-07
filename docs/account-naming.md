# Account naming

Core automatically names new accounts from compatible import, including the
admin UI and credential API. Ordinary account-file import preserves its supplied
name and rejects missing names. Manual account creation is unchanged.

The endpoints share CredentialImportHandler:

- `/api/v1/admin/accounts/import/compat`
- `/api/v1/credentials/accounts/import/compat`

Both set the internal `CreateInput.AutoName` flag. Incoming names are overridden
for new compatible accounts. Ordinary import neither accepts nor sets this flag.
Existence is checked by normalized email before allocating an index.
Reauthorization and soft-delete restoration preserve the existing name without
consuming another index, even when the plan changes. Accounts without an email
cannot be deduplicated by this identity rule.

Names retain `MMDD-Plan-index`, for example `1007-Plus-1`. The date uses
Asia/Shanghai (UTC+8). Recognized plan identities use only an uppercase initial,
for example `Plus`, `Prolite`, or `K12`. Missing and unrecognized plan types share
`Unknown`, for example `1007-Unknown-1`.

The SQL-managed `account_name_counters` table lives in Core's main database:

| Column | Meaning |
| --- | --- |
| platform | Stable platform key |
| naming_date | Full local date including year |
| plan_type | Normalized plan label; Unknown for missing/unrecognized plans |
| last_index | Last committed allocation as BIGINT |

The primary key is `(platform, naming_date, plan_type)`. Each new day and plan
starts at 1. The atomic upsert and account write share one transaction; failure
rolls back the allocation. Deleting or renaming an account never rewinds a
counter. The accounts schema is unchanged. Names remain display labels, not
globally unique identifiers: dates repeat annually and manual names can overlap.

OpenAI `refresh_token` input is exchanged first. The plugin obtains complete
credentials and resolves plan_type from JWT claims without accounts/check. JSON
containing only an RT also performs this exchange. Complete credentials are
parsed directly: access-token plan claims take precedence, with ID-token fallback
only for the same account. Uploaded plan_type is overwritten; missing claims
resolve to unknown. Free/unknown clears stale subscription expiration. They are not
refreshed merely because an RT is also present. Core names only the completed
draft. Failed exchanges do not create accounts or consume indexes. Dry runs
never allocate names.

The compatible UI now sends drafts to Core's compatible endpoint instead of the
ordinary import endpoint. Its existing RT progress flow exchanges tokens first,
then submits complete credentials to the same compatible pipeline.

The OpenAI plugin no longer has name counters or a renamed response flag.
Provider-specific email alias handling and import defaults remain separate.
The new table starts empty, with no legacy cursor backfill or compatibility mode.
Backups must include this counter table together with account data.
