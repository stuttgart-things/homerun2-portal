# sthings-portal

One frontend for a set of services — the homerun2 services today, the lab
services (clusterbook, clusterscope, machinery, claim-machinery-api) next
([#23](https://github.com/stuttgart-things/sthings-portal/issues/23)), each
as its own deployment. Formerly `homerun2-portal`. The portal is a thin shell —
layout, navigation, service status — with a reverse proxy in front of each
service UI, so everything lives under one hostname:

```
Browser ──► sthings-portal
              ├── /             overview: one card per service, live status
              ├── /ui/<key>     a service's UI inside the portal shell
              ├── /kiosk        the services full screen, one after another
              ├── /led/*   ──►  homerun2-led-catcher   (BASE_PATH=/led)
              ├── /core/*  ──►  homerun2-core-catcher  (BASE_PATH=/core)
              └── …
```

The path is forwarded **unchanged**: `/led/api/events` reaches the
led-catcher as `/led/api/events`. Each service therefore runs with
`BASE_PATH=/<key>` and answers under its prefix itself
([homerun2-led-catcher#136](https://github.com/stuttgart-things/homerun2-led-catcher/pull/136)).
Server-sent events stream through the proxy unbuffered.

## Roadmap

1. **Shell** (this) — navigation, status from each service's `/health`,
   service UIs embedded whole.
2. **`BASE_PATH` everywhere** — core-catcher, light-catcher, demo-pitcher.
3. **Fragments** — services offer `/<key>/ui/manifest.json` and
   `/<key>/ui/fragment/<id>`; the portal composes them on the overview
   (see below): led-catcher, core-catcher, light-catcher, demo-pitcher and
   notification-catcher offer one each; the navigation takes the services' titles from them.
4. **Shared theme** — services use the `--hr-*` tokens from
   `/static/homerun2-tokens.css` instead of their own palettes (see below).
   First: led-catcher.

## Fragments (the micro-frontend contract)

A service can put parts of its UI on the portal's overview. The portal reads
`GET /<key>/ui/manifest.json` with every health check:

```json
{
  "title": "LED Matrix",
  "version": "v1.2.3",
  "fragments": [
    {"id": "events", "title": "Latest on the matrix",
     "url": "/led/ui/fragment/events", "refreshSeconds": 5}
  ]
}
```

and loads each fragment into the service's card with htmx, through the proxy
(`hx-get="/led/ui/fragment/events" hx-trigger="load, every 5s"`).

A fragment:

- is plain HTML without `<html>`/`<head>`/`<script>`,
- is styled only by the portal's classes (`hr-frag`, `hr-list`, `hr-item`,
  `hr-time`, `hr-dot`, `hr-sev hr-sev-{error,warning,success,info,debug}`,
  `hr-system`, `hr-title`, `hr-frag-more`, `hr-muted` in
  `static/homerun2.css`, on the tokens in `static/homerun2-tokens.css`),
- carries the prefix on every URL (it runs on the portal's origin), and
- escapes everything that comes from messages.

The manifest's `title` names the service in the navigation, the header and
on its card, unless `PORTAL_SERVICES` sets one (`key|Title=url`); the portal
keeps the last one while the service is down. Its `version` is shown when the
service's `/health` reports none.

The portal only loads fragment URLs under the service's own prefix
(`/<key>/…`, no `..`, no quotes), at most 4 per service, refreshed no faster
than every 2 s. A service without a manifest just gets its status card.

## Shared theme

`/static/homerun2-tokens.css` holds the homerun2 palette as custom properties
(`--hr-bg`, `--hr-card`, `--hr-text`, `--hr-accent`, `--hr-error`, …) and
nothing else, so a service page can load it without the portal's layout.
`homerun2.css` imports it. `--hr-pixel` names the pixel font of the HOMERUN²
banners (Press Start 2P, which a page loads itself) and `--hr-highlight` the
amber of their subtitles; the portal's own header, section heads and card
titles use both.

A service UI:

- keeps a copy of the tokens under `:where(:root)`, so it looks the same when
  it runs without the portal,
- links `/static/homerun2-tokens.css` when the request carries
  `X-Forwarded-Prefix` (it came through the portal); the portal's `:root`
  wins over the zero-specificity copy whatever the order, so a palette change
  in the portal reaches every service, and
- builds its own colours on `var(--hr-…)` rather than on hex values.

### Embedded header

Inside the portal (`/ui/<key>` frames the service) the portal already shows
the HOMERUN² brand and the service's name, so a service drops its banner and
shows one compact line instead. Every service does it the same way:

- a script at the top of `<head>` sets the class before the first paint:
  `if (window.self !== window.top) document.documentElement.classList.add('hr-embedded')`
- under `.hr-embedded` the header is one line, at least 44px high, padding
  6px 16px, in `var(--hr-font)`, background `var(--hr-surface)`, a 1px
  bottom border `var(--hr-border)`; no logo, gradient, accent line or outer
  margin,
- on the left the tool's subtitle (e.g. "Matrix simulator") in 13px
  `var(--hr-muted)`, without the service name,
- on the right the tool's stats and actions in 12px, as outlined badges
  (`1px solid var(--hr-border)`) or plain links in `var(--hr-accent)`.

Opened on its own, also through the overview's "Standalone" link, a service
keeps its full header.

## Configuration

| Variable | Default | Description |
|----------|---------|-------------|
| `PORTAL_SERVICES` | *(empty)* | Comma-separated `key=url`, in navigation order. `key\|Title=url` sets the title. The service is served under `/<key>/`; the URL has no path |
| `PORT` | `8080` | HTTP port |
| `HEALTH_INTERVAL` | `10s` | How often each service's `/health` is checked; also the header's refresh |
| `HEALTH_TIMEOUT` | `2s` | Per check |
| `LOG_FORMAT` | `json` | `json` or `text` |
| `LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error` |
| `PORTAL_THEME` | `homerun2` | Look of this deployment: `homerun2` or `machinery` (see Branding) |
| `PORTAL_TITLE` | *(theme's)* | Header text, also the page title |
| `PORTAL_SUBTITLE` | *(theme's)* | Small line under the header text |
| `PORTAL_HEADER_IMAGE` | *(none)* | Path to a header graphic (`.svg`, `.png`, `.jpg`, `.webp`, at most 1 MiB) shown instead of the text |
| `PORTAL_FAVICON` | *(theme's)* | Path to a favicon (`.svg`, `.png`, `.ico`, at most 1 MiB); without it a built-in icon in the theme's accent colour |
| `AUTH_MODE` | `none` | `none` (open, as before), `local` or `oidc` (login, see Authentication) |
| `AUTH_USERS_FILE` | *(none)* | Users file: `name:bcrypt-hash[:group,group]` per line; required for `local`, the emergency access for `oidc` |
| `AUTH_SESSION_KEY` | *(none)* | At least 32 bytes; signs the session cookie |
| `AUTH_SESSION_TTL` | `12h` | How long a login lasts |
| `AUTH_OIDC_ISSUER` | *(none)* | `oidc`: the provider, e.g. `https://zitadel.example.com` |
| `AUTH_OIDC_CLIENT_ID`, `AUTH_OIDC_CLIENT_SECRET` | *(none)* | `oidc`: the portal's client; no secret for a public client (PKCE only) |
| `AUTH_OIDC_REDIRECT_URL` | *(none)* | `oidc`: `https://<portal>/auth/callback` |
| `AUTH_OIDC_SCOPES` | `openid profile email` | `oidc`: space-separated (`openid` is always added) |
| `AUTH_OIDC_USER_CLAIM` | `preferred_username` | `oidc`: names the user (then `email`, `sub`) |
| `AUTH_OIDC_GROUPS_CLAIM` | `groups`, then Zitadel's roles | `oidc`: list, comma string, or object whose keys are the groups; unset reads `groups` and else `urn:zitadel:iam:org:project:roles` |
| `AUTH_OIDC_ALLOWED_GROUPS` | *(any)* | `oidc`: comma-separated; only users with one of them get in |
| `AUTH_OIDC_NAME` | `Zitadel` | `oidc`: label of the login button |
| `AUTH_OIDC_CA_FILE` | *(none)* | `oidc`: PEM file with CAs to trust for the provider besides the system ones (private PKI) |
| `AUTH_KIOSK_TOKEN` | *(none)* | At least 32 bytes; lets a kiosk display in read-only (see Kiosk display) |

Known keys get a title: `led` LED Matrix, `core` Core Catcher, `light` Light
Catcher, `wled` WLED Mock, `pitch` Demo Pitcher, `omni` Omni Pitcher, `notify`
Notification Catcher.
`api`, `auth`, `branding`, `health`, `kiosk`, `static` and `ui` are the portal's own and cannot be
keys, nor can `overview` (it names the overview in `/kiosk?services=`).

## Branding

One codebase, one look per use case, chosen at deploy time:

| Theme | Look | Used by |
|---|---|---|
| `homerun2` (default) | Accent gradient banner, HOMERUN² in the pixel font, amber subtitles | `homerun2-portal` |
| `machinery` | Flat dark header with an accent line, UI font throughout, the machinery palette | `lab-portal` (clusterbook, clusterscope, machinery, claim-machinery-api) |

A theme is a palette (`static/homerun2-tokens.css`, `static/themes/<name>-tokens.css`)
plus an optional layout stylesheet (`static/themes/<name>.css`). The portal
serves the theme's palette **as `/static/homerun2-tokens.css`** — the file the
service UIs link behind the portal — so the services take on the theme too.

`PORTAL_TITLE`/`PORTAL_SUBTITLE` change the header text. `PORTAL_HEADER_IMAGE`
replaces it with a graphic, served by the portal at `/branding/header` (an SVG
with a CSP that blocks script). In Kubernetes the graphic comes from a
ConfigMap (KCL `config.headerImageConfigMap`, `config.headerImageKey`):

```bash
kubectl -n lab-portal create configmap lab-portal-branding --from-file=header.svg
```

Every theme has a favicon: a small SVG in its accent colour, built into the
portal. `PORTAL_FAVICON` replaces it with a file (`.svg`, `.png` or `.ico`,
at most 1 MiB). The portal serves it **without login** at `/branding/favicon`
(linked from every page, the login included) and at `/favicon.ico`, where
browsers look on their own, with the file's Content-Type and
`Cache-Control: public, max-age=3600`. In Kubernetes it comes from a ConfigMap
too (KCL `config.faviconConfigMap`, `config.faviconKey`, default
`favicon.svg`), mounted at `/etc/portal/favicon/`:

```bash
kubectl -n lab-portal create configmap lab-portal-favicon --from-file=favicon.svg
# or a .png/.ico (kubectl puts it under binaryData), then config.faviconKey: favicon.ico
```

Like the header graphic, a missing file or another type keeps the pod from
starting.

`docs/branding/lab-header.svg` is an example; `tests/kcl-lab-profile.yaml`
renders the lab instance with it and a favicon. A new theme is a new entry in
`internal/branding` and its files under `static/themes/`.

## Authentication

The lab services behind the portal write (IP/DNS allocation, claim orders)
and read live clusters, so the lab instance runs with a login.
`AUTH_MODE=local` checks users from a file in htpasswd's bcrypt format:

```bash
htpasswd -nbB patrick 'a-long-password' >> users      # patrick:$2y$…
# optional groups after another colon: patrick:$2y$…:lab-admins
kubectl -n lab-portal create secret generic lab-portal-auth \
  --from-file=users=./users --from-literal=session-key="$(openssl rand -base64 48)"
```

(KCL: `config.authMode: local`, `config.authSecret: lab-portal-auth`.)

- Everything but `/health`, `/static/`, `/branding/header`, `/branding/favicon`,
  `/favicon.ico` and the login needs a
  session: a page is redirected to `/auth/login?next=…` and back, an htmx
  request gets `HX-Redirect`, anything else `401`.
- The session is a signed (`HMAC-SHA256`), `HttpOnly`, `SameSite=Lax` cookie,
  `Secure` behind HTTPS (also via `X-Forwarded-Proto`); it ends after
  `AUTH_SESSION_TTL`, on logout, or when the user's line is removed (restart
  the pod after changing the Secret).
- The services get the identity as **`X-Forwarded-User`** and
  **`X-Forwarded-Groups`**. Whatever a client sends under these names is
  removed first — also with `AUTH_MODE=none`. Keep the services ClusterIP-only
  so the portal cannot be bypassed.
- Failed logins are slowed down; unknown users take as long as wrong
  passwords; the login rejects a form posted from another site.

### OIDC (Zitadel) with emergency access

`AUTH_MODE=oidc` puts a "Log in with Zitadel" button on the login page
(authorization code flow with PKCE, ID token checked for signature, issuer,
audience, expiry and nonce). The users file stays next to it as **emergency
access** under "Emergency access (local user)", for when the provider is down
or misconfigured:

- The provider is looked up on first use, not at startup — the portal starts
  and the local login works even while Zitadel is unreachable; the button then
  says so.
- The KCL manifests mount `users` from `authSecret` in `oidc` mode too, so a
  deployment without emergency access does not start. Without KCL the file
  may be left out.
- An OIDC session is void once the portal runs without `oidc`; a local session
  needs the user's line, as above.
- Logout also ends the session at the provider (`end_session_endpoint`) and
  returns to `/auth/login`.

At the provider register a web application with
redirect URI `https://<portal>/auth/callback` and post-logout redirect URI
`https://<portal>/auth/login`. For Zitadel, turn on the project's role
assertion and the app's "roles in ID token" so the groups arrive in
`urn:zitadel:iam:org:project:roles`, then limit the portal with
`AUTH_OIDC_ALLOWED_GROUPS` (KCL `config.authOidcAllowedGroups`):

```bash
kubectl -n lab-portal create secret generic lab-portal-auth \
  --from-file=users=./users --from-literal=session-key="$(openssl rand -base64 48)" \
  --from-literal=oidc-client-secret="$(terraform output -raw client_secret)"
```

A Zitadel whose certificate comes from a private PKI (the lab's Vault, a
self-signed issuer) needs its CA, or the login ends in "not reachable" with an
`x509` error in the log. Put the CA into a ConfigMap and name it in
`config.authOidcCaConfigMap` (key `config.authOidcCaKey`, default `ca.crt`);
the portal trusts it besides the system CAs (`AUTH_OIDC_CA_FILE`):

```bash
kubectl -n lab-portal create configmap lab-portal-oidc-ca --from-file=ca.crt=./vault-pki-ca.pem
```

### Kiosk display

A box that boots into a browser and rotates `/kiosk` (the edge kiosk of
`sthings.baseos.kiosk`) cannot log in. With `AUTH_KIOSK_TOKEN` set (KCL: key
`kiosk-token` in `authSecret`), it opens the kiosk URL with the token once:

```
https://<portal>/kiosk?services=overview,clusterbook&seconds=30&kiosk_token=<token>
```

- The portal sets a session for the user **`kiosk`** (group `kiosk`) and
  redirects to the same URL without the token.
- The session only **reads**: `GET`/`HEAD`/`OPTIONS`, everything else is `403`
  — nobody at the display assigns an IP or orders a claim.
- It renews itself while pages load, so the rotation does not end on the login
  page; a new token ends all kiosk sessions; a wrong token goes to the login.
- Works with `local` and `oidc`. The token is a password: keep it out of public
  kiosk config files (`openssl rand -hex 32`).

## Endpoints

| Path | |
|------|-|
| `/` | Overview |
| `/ui/<key>` | A service inside the portal |
| `/kiosk` | Kiosk: the services full screen, one after another (see below) |
| `/kiosk/overview` | The overview's cards without header and navigation |
| `/ui/status` | Status pills (htmx partial) |
| `/api/services` | Service status as JSON |
| `/health` | The portal's own health and build info |
| `/static/homerun2.css` | Portal layout and fragment classes |
| `/static/homerun2-tokens.css` | Design tokens only, for the service UIs |
| `/branding/header`, `/branding/favicon`, `/favicon.ico` | Header graphic, favicon (public) |
| `/<key>/…` | Proxied to the service |

## Kiosk

`/kiosk` is for an office display (the sthings-edge box): a slim banner with
the current service, the status pills and a progress bar, below it one
service UI full screen. It switches to the next service every `seconds`,
skips services that are down (the overview never is), and holds the current
one while someone uses keyboard, mouse or touch, also inside the service UI.
At most two frames exist — the one shown and the next one loading — so the
switch never shows a blank page and the event streams of hidden UIs do not
pile up.

```
/kiosk?services=overview,core,led&seconds=30&pause=60&reset=300&zoom=led:130
```

| Parameter | Default | |
|-----------|---------|-|
| `services` | every service, in navigation order | Comma-separated keys; `overview` is the overview |
| `seconds` | `30` | Per service; `0` stays on the first |
| `pause` | `60` | No switch within N s after input; `0` is off |
| `reset` | `0` (off) | Reload the kiosk N s after the last input: back to the first service |
| `zoom` | `100` | Percent: `130` for every service, `led:130,core:80` per service |

Seconds are `0` or 5 (`pause`, `reset`: 1) to 86400, zoom 25 to 400; anything
else, or an unknown key, is a 400 that says why.

## Development

```bash
go test ./...

# led-catcher under /led on :8099, then the portal on :8090
LED_MODE=standalone BASE_PATH=/led LED_API_TOKEN=dev HEALTH_PORT=8099 python -m led_catcher
task run-local            # PORTAL_SERVICES=led=http://localhost:8099

task lint                 # golangci-lint via Dagger
task build-test-binary    # unit tests + binary against a stub service via Dagger
task build-scan-image-ko  # ko image + Trivy
```

## Release

Conventional commits on `main` drive semantic-release: after the image build
on `main` succeeds, the `Release` workflow tags `vX.Y.Z`, publishes the
GitHub release, stages the image and pushes the kustomize base to
`ghcr.io/stuttgart-things/sthings-portal-kustomize`. Same pipeline as the
other homerun2 Go services.

## Deployment

KCL manifests in `kcl/`; see `tests/kcl-deploy-profile.yaml`:

```yaml
config.services:
  - led=http://homerun2-led-catcher.homerun2.svc.cluster.local
config.httpRouteEnabled: true
config.httpRouteHostname: homerun2.example.com
```

The portal's HTTPRoute takes the whole hostname; the services behind it need
no route of their own.
