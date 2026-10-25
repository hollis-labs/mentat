# Security policy

## Supported versions

Stack Explorer is pre-1.0 software (tagged `0.x`). Security fixes are made on
`main` and released as a new patch version. There are no backports.

## Report a vulnerability

Do not include an exploit, token, database, or other sensitive material in a
public issue.

Use GitHub's private vulnerability-reporting flow when the repository's
Security tab offers it. If it is unavailable, contact a repository maintainer
privately through a contact channel published on the Hollis Labs organization
or maintainer profile. Include:

- the affected commit and operating system
- the surface involved (`serve` REST API, `mcp` stdio or HTTP, CLI)
- the listen address and whether a token was set
- reproduction steps and the security impact
- a safe way to contact you about coordination

Maintainers will acknowledge a private report, investigate it, and coordinate
disclosure; response times are best effort.

## Deployment boundary

Stack Explorer is a local research tool for one user on one machine.

- **`stack-explorer serve`** binds `127.0.0.1` by default and accepts browser
  requests only from the configured local frontend origin (`--cors-origin`). On
  loopback no token is needed. Binding any other interface is refused unless a
  bearer token is set with `--token` or `STACK_EXPLORER_API_TOKEN`.
- The token is a single shared secret, and Stack Explorer provides **no TLS**.
  For anything beyond a trusted network, put it behind a TLS-terminating
  reverse proxy, a VPN, or an SSH tunnel.
- **`stack-explorer mcp --transport http`** defaults to `127.0.0.1` and has **no
  authentication**. Keep it on loopback. The stdio transport trusts whichever
  process launched it.
- The MCP surface includes write tools for findings and audits; run it only for
  clients you trust with the catalog.

## Data at rest

The catalog is a SQLite database (`data/stack-explorer.db` or
`~/.stack-explorer/stack-explorer.db`). It can hold repository metadata, audit
findings and code symbols, including from private repositories you import, and
it is not encrypted. Protect it with filesystem permissions, and do not share
it without checking what it contains. Keep your own `repos.yaml` (gitignored)
out of public commits.

## External data processors

Scans and embedding refresh make outbound calls only when you use them: the
GitHub API for repository metadata (using `GITHUB_TOKEN` when set), and an
embedding provider when embedding refresh is configured, which receives the text
being embedded. Nothing is sent otherwise.

## Current security limitations

- no built-in TLS
- a single shared bearer token with no per-caller authorization
- unauthenticated MCP HTTP transport
- no at-rest encryption
- pre-release contracts and schema
