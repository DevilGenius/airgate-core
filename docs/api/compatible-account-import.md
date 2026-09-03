# Compatible account credential import API

AirGate provides a stable, atomic API for parsing compatible credential files and importing the resulting upstream accounts. Callers do not need access to the generic plugin RPC API.

## Endpoints

- Administrator JWT or `admin-` key: `POST /api/v1/admin/accounts/import/compat`
- Credential management key: `POST /api/v1/credentials/accounts/import/compat`

Use `Authorization: Bearer <key-or-token>`.

## Request

The request must use `multipart/form-data`:

| Field | Required | Description |
| --- | --- | --- |
| `platform` | yes | Target platform, for example `openai`. |
| `format` | yes | Parser format advertised by the active platform plugin, for example `codex`. |
| `dry_run` | no | `true` validates and previews without writing to the database. Defaults to `false`. |
| `files` | yes | One or more credential files. Repeat this field for multiple files. |

For OpenAI refresh-token import, use `format=refresh_token` (or the `rt` alias). Each file may contain one token, multiple whitespace-separated tokens, or a JSON object. Token exchange and account import are completed in the same request.

Only `refresh_token` is required in the JSON form:

| Field | Required | Description |
| --- | --- | --- |
| `refresh_token` | yes | Refresh token to exchange for a complete OpenAI OAuth credential. |
| `name` | no | Account name to preserve. When omitted, AirGate generates the account name. |
| `client_id` | no | OAuth client ID override. When omitted, the OpenAI plugin uses its default client ID. |
| `proxy_url` | no | Proxy used during token exchange. When omitted, no explicitly assigned proxy is used. |

Minimal JSON input:

```json
{
  "refresh_token": "rt_..."
}
```

Complete JSON input:

```json
{
  "name": "Custom account name",
  "refresh_token": "rt_...",
  "client_id": "optional-client-id",
  "proxy_url": "http://optional-proxy:8080"
}
```

For plain-text input, provide one or more refresh tokens separated by whitespace. Plain-text tokens use automatic account names and the default client ID without an explicitly assigned proxy.

Limits:

- At most 1024 files and 1024 parsed accounts per request.
- At most 32 MiB of file content per request.
- Uploaded content is processed in memory and is not included in audit logs.

Example:

```bash
curl -X POST "https://airgate.example/api/v1/credentials/accounts/import/compat" \
  -H "Authorization: Bearer cred-..." \
  -F "platform=openai" \
  -F "format=codex" \
  -F "dry_run=true" \
  -F "files=@account.auth.json;type=application/json"
```

## Response

The response uses the standard AirGate envelope. Its `data` value follows contract version 1:

```json
{
  "code": 0,
  "message": "ok",
  "data": {
    "contract_version": 1,
    "platform": "openai",
    "format": "codex",
    "dry_run": false,
    "parsed": 2,
    "imported": 1,
    "failed": 1,
    "issues": [
      {
        "stage": "import",
        "index": 1,
        "name": "account@example.com",
        "level": "error",
        "message": "..."
      }
    ]
  }
}
```

`stage` is one of `parse`, `validation`, or `import`. A successful HTTP response can still contain item-level failures; inspect `failed` and `issues`.

The Core resolves parsers by the plugin capability `account_import.v1`, the requested platform, and format. Plugin names are not part of the public contract.

## Delete one account

Both credential-management and administrator interfaces expose a single-account
delete endpoint:

- `POST /api/v1/credentials/accounts/delete`
- `POST /api/v1/admin/accounts/delete`

The request body accepts only the account primary key:

```json
{"id": 123}
```

The operation is a soft delete; names, emails, credentials, and batch ID lists
are not accepted.

## Ban one account

Both credential-management and administrator interfaces expose a single-account
ban endpoint:

- `POST /api/v1/credentials/accounts/ban`
- `POST /api/v1/admin/accounts/ban`

The request body uses the same account primary-key form:

```json
{"id": 123}
```

Only an account that has not been soft-deleted is changed. Its state becomes
`disabled` and its state detail is set to `Banned`.
