# ADR 0001: External Go modules for authentication

- **Status:** accepted
- **Date:** 2026-10-25
- **Context issues:** #23 (sthings-portal), #24 (authentication)

## Context

The portal has no external Go dependencies so far — stdlib `net/http`,
`html/template`, `httputil.ReverseProxy`. `CLAUDE.md` keeps it that way
"unless there is a strong reason".

As sthings-portal (#23) the portal also fronts the lab services
(clusterbook, clusterscope, machinery, claim-machinery-api). Those write —
IP and DNS allocation, claim orders — and read live clusters. Under one
hostname the portal is the single way in, so it authenticates (#24):

- OIDC (authorization code + PKCE) against Zitadel as the primary login
- local users as a fallback, passwords stored as bcrypt hashes

Hand-rolling either is security-critical code: ID token signature
validation, JWKS fetching and key rotation, issuer/audience/nonce/expiry
checks, discovery, and a constant-time password hash.

## Decision

Authentication is the strong reason. The portal may depend on:

| Module | For |
|---|---|
| `github.com/coreos/go-oidc/v3` | discovery, JWKS, ID token verification |
| `golang.org/x/oauth2` | authorization code flow, PKCE, token exchange |
| `golang.org/x/crypto` (`bcrypt`) | local user password hashes |

The exception is scoped to authentication. The rest of the portal stays
stdlib-only; any further module needs its own ADR.

## Consequences

- Renovate keeps the three modules current; security updates are merged
  promptly like any other dependency.
- `internal/auth` is the only package that imports them.
- The image grows slightly; the build (ko) is unchanged.
- Rejected: hand-rolled OIDC (too much security-critical code to own);
  oauth2-proxy as the only path (no local-user fallback, no htmx-aware
  `HX-Redirect`) — it remains an option in front of a single service.
