# Real-Estayer

A travel-planning app: search and track Airbnb listings, build trips, and enrich
destinations with weather, transit, visas, events, flights, and more.

Two services:

| Service | Language | Role |
| --- | --- | --- |
| **app** (`cmd/server`) | Go 1.25 | Web app + JSON API. Server-rendered HTML, MongoDB. |
| **rust-scraper** (`rust-scraper/`) | Rust | Headless-Chrome Airbnb scraper. Writes listings into the same MongoDB. |

The Go app runs fine without the scraper — listing search just returns whatever
is already in the database.

---

## Quick start

```bash
make env          # copies .env.example -> .env
# edit .env: set SESSION_SECRET and SCRAPER_API_KEY (see below)
make docker-up    # mongo, redis, app on http://localhost:8347
```

Two values are worth setting before first boot:

```bash
openssl rand -hex 32   # -> SESSION_SECRET
openssl rand -hex 32   # -> SCRAPER_API_KEY
```

In development both are optional: a blank `SESSION_SECRET` generates an
ephemeral one at startup (sessions die on restart). In production the app
**refuses to start** without `SESSION_SECRET` and `SCRAPER_API_KEY`, and
rejects `ALLOWED_ORIGINS=*`.

To run the app on the host instead of in Docker:

```bash
make docker-up      # then stop just the app container: docker compose stop app
make dev            # live reload via air, falls back to `go run` if absent
```

Schema migrations run automatically at startup (`internal/migrations`), so
there is no separate migrate step for normal use.

### Everything else

`make help` lists every target. The ones you'll actually use:

```
make dev              # live-reload server
make test             # go test ./...
make lint             # golangci-lint
make fmt              # gofmt + goimports
make docker-up-build  # rebuild images and restart
make docker-logs-app  # tail app logs
make mongo-shell      # mongosh into the running container
make docker-admin     # also start mongo-express on :8081
```

---

## Configuration

`.env.example` is the reference — every variable is commented there with what
it does and how to get a key. Only these matter to get running:

| Variable | Needed for |
| --- | --- |
| `SESSION_SECRET` | Required in production. 32+ chars. |
| `SCRAPER_API_KEY` | Required in production, and by the scraper (it refuses to start without one). Must match on both sides. |
| `MONGODB_URI` / `MONGODB_DATABASE` | Defaults point at the compose network. |
| `ALLOWED_ORIGINS` | CORS. Cannot contain `*` in production. |
| `ADMIN_BOOTSTRAP_EMAILS` | Emails that become admin on first registration. Dev only. |

**Every other key is optional and degrades gracefully.** Each enrichment
service returns an empty result (or a 503) when its key is unset, so an
unconfigured install still runs — you just get fewer widgets. The app logs a
feature summary at boot (`config.LogFeatureSummary`) telling you exactly which
integrations came up configured. Read that instead of auditing `.env`.

Nothing outside `SESSION_SECRET`, `SCRAPER_API_KEY`, and the Mongo URI is
required to boot.

---

## Architecture

Layered, no framework magic, no code generation, no ORM:

```
cmd/server/main.go        wiring + all route definitions (~150 routes)
    │
internal/handler/         HTTP: decode, authorize, call a service, render
    │
internal/service/         business logic + third-party API clients
    │
internal/repository/      MongoDB queries
    │
internal/models/          BSON structs
```

Supporting packages:

| Package | Role |
| --- | --- |
| `internal/config` | Env loading + production validation |
| `internal/middleware` | Auth, CSRF, rate limiting, request logging, metrics, panic recovery |
| `internal/migrations` | Ordered, idempotent, versioned schema migrations |
| `internal/provider` | Flight providers behind an interface (`duffel/`), plus `wikipedia/` |
| `internal/listing` | Amenity/feature/fact normalization for scraped listings |
| `internal/crypto` | AES-GCM field encryption for passport / KTN / loyalty numbers |
| `internal/dbtest` | Test helper that skips unless `MONGODB_TEST_URI` is set |
| `web/templates` | `html/template` pages, layouts, partials |
| `web/static` | One `app.css`, one `app.js` |

**Routes all live in `cmd/server/main.go`.** It reads long, but it's the single
place to answer "what endpoints exist and what middleware do they carry."

### Frontend

Server-rendered `html/template` + Alpine.js and Tailwind from CDN. **There is
no build step and no `npm`** — edit a template or `web/static/app.js` and
refresh. Keep it that way; it's the reason setup is one command.

### Handler dependencies

`Handler` needs ~45 services. They're grouped by domain in `HandlerDeps`
(`internal/handler/handler.go`) and embedded into `Handler`, so adding a
service means adding **one field to one group struct**:

```go
h.Core.Auth          // identity, credentials, audit, photo storage
h.Listings.Scraper   // search, scraping, watchlists, price history
h.Trips.Expense      // trips + collaboration, expenses, journals, polls
h.Enrich.Weather     // read-only third-party lookups
h.Flight             // flight booking
h.Config
```

New enrichment integration? Add it to `EnrichDeps`, construct it in `main.go`,
done. Nothing else changes.

---

## Adding a feature

The pattern is consistent across 19 repositories and 46 services. Copy the
nearest neighbour:

1. **Model** — `internal/models/foo.go`, BSON tags.
2. **Repository** — `internal/repository/foo_repo.go`, register in `repositories.go`.
3. **Service** — `internal/service/foo_service.go`, business logic only.
4. **Handler** — a method on `*Handler`; use `h.jsonResponse` / `h.jsonError` /
   `h.getUserID` / `h.render` rather than writing to `w` directly.
5. **Deps** — add the service to the matching group in `HandlerDeps`, construct
   it in `main.go`.
6. **Route** — register in `cmd/server/main.go` under the right middleware group.
7. **Template** — `web/templates/pages/foo.html` if it renders a page.

Schema change? Add `internal/migrations/mNNN_thing.go` with the next version
number and `Register()` it from `init()`. Migrations must be idempotent — they
run on every boot and only unapplied versions execute.

---

## Testing

```bash
make test                                        # unit tests only
MONGODB_TEST_URI=mongodb://127.0.0.1:27017 make test   # + integration tests
```

Integration tests (`internal/repository`, `internal/migrations`,
`internal/database`) **silently skip** without `MONGODB_TEST_URI`. If a
repository change looks suspiciously well-tested, check you set it.

CI (`.github/workflows`) runs a real MongoDB 7 service, so integration tests
always execute there. Hard gates: `gofmt`, `go vet`, `go build`,
`go test -race`, and `golangci-lint`. `gosec` and `govulncheck` are advisory —
gosec's remaining findings need case-by-case triage (e.g. `crypto/sha1` is
correct for TOTP) and govulncheck depends on an upstream DB that can fail for
unrelated reasons.

A separate CI job covers Rust: `cargo fmt --check`, `cargo clippy --no-deps
--all-targets -- -D warnings`, build, and both test targets. **Clippy is a hard
gate with `-D warnings`**, so a lint added by a newer toolchain fails the build
on a file nobody touched. Run it locally before pushing:

```bash
cd rust-scraper && cargo clippy --no-deps --all-targets -- -D warnings
```

Most services are concrete structs rather than interfaces. Services that talk
HTTP take a base URL, so tests point them at an `httptest.Server` — see
`internal/service/http_services_test.go` for the established pattern.

---

## The Rust scraper

**It runs on the host, not in compose.** The stealth flow depends on driving a
real Chrome, so the scraper lives on the Windows box and the app reaches it over
the LAN via `RUST_SCRAPER_URL`. `make docker-up` starts mongo, redis, and the
app only.

```bash
cd rust-scraper
./fetch-chromedriver.sh                       # match ChromeDriver to local Chrome
SCRAPER_API_KEY=<same-as-app> cargo run       # binds 127.0.0.1:3001
```

Point the app at it with `RUST_SCRAPER_URL`. `SCRAPER_API_KEY` is mandatory —
the scraper refuses to start without one, and the Go app sends it as
`X-API-Key`. `/health` is deliberately exempt from that middleware.

There *is* a `rust-scraper` compose service, but it sits behind a profile and is
never built or started by default:

```bash
docker compose --profile scraper up -d      # dev/CI only
```

It exists so the stack can be brought up standalone and so the Dockerfile stays
exercised rather than rotting. Note the container drives headless Chromium, not
the host's Chrome, so it is not a substitute for the host process when you are
iterating on scraping.

### ChromeDriver is a recurring chore

ChromeDriver must match Chrome's **major** version, and Chrome auto-updates. So
this will break periodically with:

```
This version of ChromeDriver only supports Chrome version 139
```

Fix: re-run `./fetch-chromedriver.sh`. It detects the installed Chrome, resolves
the matching driver from Chrome for Testing, and installs it next to the
scraper. Windows has `update-chromedriver.ps1` / `setup-chrome.ps1`.

The binary is **deliberately not committed** — it's ~15MB, platform-specific,
and goes stale. Override the location with `CHROME_DRIVER_PATH`. See
`CHROMEDRIVER_UPDATE.md` for manual steps and port-9515 conflicts.

### Toolchain note

`.cargo/config.toml` has a commented-out `-Z threads=16` that speeds up builds
but requires **nightly** — with it enabled, every cargo command fails on
stable. Uncomment only if you're on nightly.

### Three scrape paths

`rust-scraper/src/scraping/` has three deliberately separate flows:

| Path | Driver | Notes |
| --- | --- | --- |
| `fast_path.rs` | HTTP / light | Fastest. Rotates UA, viewport, headers; jitters requests. |
| `browser_flow.rs` | WebDriver (`thirtyfour`) | Legacy. Simulated mouse movement and human-like scrolling. |
| `stealth_flow.rs` | CDP (`chromiumoxide`) | Hardest to detect — bypasses the WebDriver protocol entirely. |

**Their search-URL builders are not interchangeable** and must not be merged —
each produces different Airbnb results. `build_stealth_search_url` in
`stealth_flow.rs` documents the three-way difference; read that comment before
touching any of them. The exact URLs are pinned by tests.

### Browser identity

`scraping/profile.rs` owns every claim a fingerprinting check can cross-check:
user agent, `navigator.platform`, `Sec-CH-UA*`, `navigator.userAgentData`,
`Accept`, `Accept-Language`. They only ever travel together as a
`BrowserProfile`, and `profile::active()` pins one for the process lifetime.

Two rules, both enforced by tests:

- **Never mix fields between profiles.** Independent draws are what let a
  Firefox UA ship with Chrome client hints.
- **Profiles are filtered to the host OS.** Claiming `Win32` on Linux
  contradicts the WebGL renderer and font metrics, which are not cheap to fake.

The identity is deliberately stable within a run — a browser that changes its
user agent between requests in one session is more suspicious than one that
never does. Rotation happens across restarts.

**The claimed Chrome version is read from the installed binary**
(`detect_chrome_version`), so the user agent tracks reality instead of drifting
out of a hardcoded table. `CHROME_VERSION_OVERRIDE` pins it; if detection fails
the scraper logs a warning and falls back to `FALLBACK_FULL_VERSION`.

Profiles are **Chrome-only on purpose**. The CDP transport only speaks to
Chromium, `STEALTH_JS` installs a `window.chrome` object, and
`navigator.userAgentData` exists nowhere else — so a Firefox or Safari identity
would contradict the transport itself.

The CDP path applies this via `Emulation.setUserAgentOverride` rather than
injected JS, so Chrome sets it below the JS layer, and registers its stealth
script with `Page.addScriptToEvaluateOnNewDocument` so it runs before any page
script rather than after `goto()` returns.

### Egress and rate

`SCRAPER_PROXIES` (comma-separated) round-robins both the browser
(`--proxy-server`) and the HTTP client through several addresses. Unset means
direct connections. Round-robin rather than random: with a small pool, sampling
clusters requests onto whichever address luck favours, which is the pattern
we're avoiding. Credentials are redacted before any URL is logged.

Request rate from a single IP is the strongest signal a target has, and no
amount of fingerprint work substitutes for spreading it. `MAX_CONCURRENT_SCRAPES
= 1` is the other half of this.

### Known remaining gap: TLS fingerprint

The HTTP fast path uses `reqwest` + `rustls`, whose JA3/TLS ClientHello does
**not** match Chrome's — wrong extension order, no GREASE. Airbnb can see this.
The browser paths are unaffected, since they drive real Chrome.

Closing it means swapping the fast path onto a TLS-impersonating client
(`rquest`, or `curl-impersonate`), which is a dependency change, not a
configuration one. What *has* been aligned at this layer:

- **HTTP/2 is enabled.** `reqwest`'s `http2` feature is in its defaults but this
  crate sets `default-features = false`, so the fast path previously spoke
  HTTP/1.1 only while sending a Chrome user agent — visible both in the ALPN list
  advertised during the handshake and to the server directly.
- **A cookie jar is enabled**, so session cookies are echoed back. Without one
  every request looked like a first visit from a browser that should have state.
- **`Accept-Encoding` is real** (gzip/br/deflate/zstd) rather than the previous
  `identity`, which no browser ever sends.

Extraction is layered so a single DOM change degrades rather than breaks:
embedded Airbnb JSON → JSON-LD → regex over page source → DOM query. When a
scrape returns titles but no prices, an extractor in
`scraping/extract.rs` has gone stale — that's the routine failure mode.

---

## Known maintenance load

Honest about where the time goes:

- **~40 external integrations.** They break without you touching anything.
  Most fail soft, so watch the boot feature summary and the error webhook.
- **The scraper is the real cost centre.** Airbnb changes markup, Chrome
  updates, anti-bot measures shift. Budget for this, not for the Go app.
- **`mongodb` is exposed on `0.0.0.0:27017`** in compose so an off-box scraper
  can write directly. Fine for LAN dev; lock it down for production and make
  the scraper authenticate or write through an HTTP API.
