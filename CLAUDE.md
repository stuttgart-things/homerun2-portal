# CLAUDE.md

## Project

sthings-portal (formerly homerun2-portal) — Go service that is the single web frontend for a set of services, deployed once per set (homerun2 today, the lab services next, see #23): a shell (layout, navigation, service status) plus a reverse proxy that serves each service UI under its own prefix (`/led/`, `/core/`, …). First step of a micro-frontend approach: service UIs are embedded whole today and are to be composed from HTML fragments later.

## Tech Stack

- **Language**: Go 1.27+
- **HTTP**: stdlib `net/http` (no framework), `httputil.ReverseProxy`
- **UI**: `html/template` + htmx (vendored, `static/htmx.min.js`)
- **Build**: ko (`.ko.yaml`), no Dockerfile
- **CI**: Dagger modules (`dagger/main.go`), Taskfile
- **Deploy**: KCL manifests (`kcl/`), Kubernetes
- **Infra**: GitHub Actions, semantic-release, renovate

No external Go dependencies — keep it that way unless there is a strong reason. The one exception is authentication (`go-oidc`, `x/oauth2`, `x/crypto/bcrypt`, only in `internal/auth`), see [ADR 0001](docs/adr/0001-external-go-modules-for-auth.md); any further module needs its own ADR.

## Git Workflow

**Branch-per-issue with PR and merge.**

### Branch naming

- `fix/<issue-number>-<short-description>` for bugs
- `feat/<issue-number>-<short-description>` for features
- `test/<issue-number>-<short-description>` for test-only changes
- `chore/<issue-number>-<short-description>` for infra/CI changes

### Commit messages

- Use conventional commits: `fix:`, `feat:`, `test:`, `chore:`, `docs:`
- Include `Closes #<issue-number>` to auto-close issues

## Code Conventions

- No Dockerfile — use ko for image builds
- Config via environment variables, loaded once at startup (`internal/config`); invalid values fail startup
- Tests: `go test ./...` — no network beyond `httptest`
- Logging: `log/slog` (JSON/text)

## Architecture

```
Browser ──► mux ──┬── GET /, /ui/<key>, /ui/status, /api/services,  (internal/web)
                  │       /kiosk, /kiosk/overview
                  ├── GET /health                                   (internal/handlers)
                  ├── GET /static/                                  (static/, embedded)
                  └── /<key>/…  ──► ReverseProxy ──► service         (internal/proxy)

health.Checker ── polls <service>/health every HEALTH_INTERVAL ──► status shown by web
```

### The fragment contract

A service may offer `GET /<key>/ui/manifest.json` with its `title`, `version` and fragments (`id`, `title`, `url`, `refreshSeconds`). The title names the service in navigation, header and card unless `PORTAL_SERVICES` sets one (`Service.TitleSet`). The overview loads each fragment into the service's card with htmx through the proxy. Fragments are bare HTML styled only by the `hr-*` classes in `static/homerun2.css`; see the README. Keep the URL checks in `validFragments`: a manifest must not be able to point the browser outside its service's prefix.

### The prefix contract

The proxy forwards the request path **unchanged** (`/led/x` → service `/led/x`) and sets `X-Forwarded-Prefix`. Every service runs with `BASE_PATH=/<key>`, answers under it, and puts it on every URL it hands to the browser. `/health` stays at the service's root. Do not add prefix stripping to the proxy — a service that does not know its prefix hands out URLs without it.

## Key Paths

- `main.go` — entrypoint, mux wiring (`newMux`), graceful shutdown
- `internal/services/registry.go` — `PORTAL_SERVICES` parsing, key rules, known titles
- `internal/proxy/proxy.go` — per-service reverse proxy (unbuffered for SSE, 502 page)
- `internal/health/health.go` — concurrent `/health` polling, status snapshot
- `internal/health/manifest.go` — reads `/<key>/ui/manifest.json`: title and version (cleaned to one line), and only fragment URLs under the service's prefix
- `internal/web/` — templates (`layout`, `index`, `service`, `status` partial, `bare`, `kiosk`) and handlers
- `internal/web/kiosk.go`, `static/kiosk.js` — `/kiosk`: query options (checked server-side, 400 on anything unknown) and the rotation in the browser (skips down services, at most two iframes, input pauses)
- `internal/auth/` — login (`AUTH_MODE=local` or `oidc`): users file, signed session cookie, `Middleware` in front of the whole mux, `X-Forwarded-User`/`-Groups` to the services. Identity headers from clients are **always** stripped (`StripIdentity`, also without auth). `oidc.go`: code flow + PKCE against Zitadel; the users file stays the **emergency access** — keep provider discovery lazy (never at startup) so the local login works while the provider is down. The only package that may import `x/crypto`, `go-oidc`, `x/oauth2` (ADR 0001)
- `internal/branding/` — theme presets (`Themes`), `PORTAL_THEME`/`TITLE`/`SUBTITLE`/`HEADER_IMAGE`; the theme's palette is served **as** `/static/homerun2-tokens.css` so the services follow the theme — keep that path, the service UIs link it
- `static/themes/` — per-theme palette (`<name>-tokens.css`, same custom properties as `homerun2-tokens.css`) and layout (`<name>.css`, loaded after `homerun2.css`)
- `static/homerun2-tokens.css` — design tokens (`--hr-*`) only, dark by default; service UIs link it behind the portal (README "Shared theme"), so keep rules out of it
- `static/homerun2.css` — portal layout (retro banner look of the service UIs: accent gradient, `--hr-pixel` font) and the fragment `hr-*` classes; imports the tokens
- `static/sthings-logo.png` — the stuttgart-things logo of the nav footer's credit (vendored, no request to GitHub)
- `kcl/` — deployment manifests; `config.services` → `PORTAL_SERVICES`
- `dagger/main.go` — Lint, Build, BuildImage, ScanImage, BuildAndTestBinary (against an nginx stub service)
- `.github/workflows/` — `build-test` (Dagger), `build-scan-image` (ko image per PR and on main), `release` (semantic-release + kustomize OCI after a green image build on main)
- `.releaserc`, `.goreleaser.yaml` — release config; versions come from conventional commits
- `catalog-info.yaml` — Backstage component (system homerun2)

## Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `PORTAL_SERVICES` | *(empty)* | Comma-separated `key=url` (or `key\|Title=url`), in navigation order |
| `PORT` | `8080` | HTTP port |
| `HEALTH_INTERVAL` | `10s` | Status poll interval, also the header's refresh |
| `HEALTH_TIMEOUT` | `2s` | Per status check |
| `LOG_FORMAT` | `json` | `json` or `text` |
| `LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error` |
| `PORTAL_THEME` | `homerun2` | `homerun2` or `machinery` (`internal/branding`) |
| `PORTAL_TITLE`, `PORTAL_SUBTITLE` | *(theme's)* | Header text |
| `PORTAL_HEADER_IMAGE` | *(none)* | Header graphic file instead of the text, served at `/branding/header` |
| `AUTH_MODE` | `none` | `none`, `local` or `oidc` (`internal/auth`) |
| `AUTH_USERS_FILE`, `AUTH_SESSION_KEY`, `AUTH_SESSION_TTL` | *(none)*, *(none)*, `12h` | Users file (htpasswd bcrypt, optional `:groups`; emergency access with `oidc`), cookie signing key (≥ 32 bytes), login lifetime |
| `AUTH_OIDC_ISSUER`, `AUTH_OIDC_CLIENT_ID`, `AUTH_OIDC_CLIENT_SECRET`, `AUTH_OIDC_REDIRECT_URL` | *(none)* | `oidc`: provider, client, `https://<portal>/auth/callback` |
| `AUTH_OIDC_GROUPS_CLAIM`, `AUTH_OIDC_ALLOWED_GROUPS`, `AUTH_OIDC_USER_CLAIM`, `AUTH_OIDC_SCOPES`, `AUTH_OIDC_NAME` | `groups` then `urn:zitadel:iam:org:project:roles`, *(any)*, `preferred_username`, `openid profile email`, `Zitadel` | Groups claim, who may log in, user name claim, scopes, button label |

## Testing

```bash
go test ./...
task run-local
task lint
task build-test-binary
task build-scan-image-ko
```

## Reference Projects

- `homerun2-led-catcher` — first service with `BASE_PATH` (Python/FastAPI)
- `homerun2-demo-pitcher` — the Go service layout this repo follows
- `homerun2-core-catcher`, `homerun2-light-catcher` — next services to get `BASE_PATH`
