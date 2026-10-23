# homerun2-portal

One frontend for the homerun2 services. The portal is a thin shell —
layout, navigation, service status — with a reverse proxy in front of each
service UI, so everything lives under one hostname:

```
Browser ──► homerun2-portal
              ├── /             overview: one card per service, live status
              ├── /ui/<key>     a service's UI inside the portal shell
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

Known keys get a title: `led` LED Matrix, `core` Core Catcher, `light` Light
Catcher, `wled` WLED Mock, `pitch` Demo Pitcher, `omni` Omni Pitcher, `notify`
Notification Catcher.
`api`, `health`, `static` and `ui` are the portal's own and cannot be keys.

## Endpoints

| Path | |
|------|-|
| `/` | Overview |
| `/ui/<key>` | A service inside the portal |
| `/ui/status` | Status pills (htmx partial) |
| `/api/services` | Service status as JSON |
| `/health` | The portal's own health and build info |
| `/static/homerun2.css` | Portal layout and fragment classes |
| `/static/homerun2-tokens.css` | Design tokens only, for the service UIs |
| `/<key>/…` | Proxied to the service |

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
`ghcr.io/stuttgart-things/homerun2-portal-kustomize`. Same pipeline as the
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
