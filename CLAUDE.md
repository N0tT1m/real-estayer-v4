# CLAUDE.md

Guidance for working in this repo. Read `README.md` for setup; this file covers
conventions and traps.

## Layout

Go web app (`cmd/server`, `internal/`) plus a separate Rust Airbnb scraper
(`rust-scraper/`). Strict layering — do not skip a layer:

```
handler  →  service  →  repository  →  models
```

Handlers do HTTP only (decode, authorize, call one service, render). Business
logic and third-party API clients live in `internal/service`. MongoDB queries
live in `internal/repository`. **All routes are in `cmd/server/main.go`.**

## Commands

```bash
make dev                                              # live-reload server
go build ./... && go vet ./... && make test           # what CI gates on
gofmt -w .                                            # CI fails on any drift
MONGODB_TEST_URI=mongodb://127.0.0.1:27017 make test  # include integration tests
cd rust-scraper && RUSTFLAGS="" cargo check           # Rust
```

## Conventions

- **Handler dependencies are grouped.** `HandlerDeps` in
  `internal/handler/handler.go` holds `Core` / `Listings` / `Trips` / `Enrich`
  sub-structs and is embedded in `Handler`. Access is `h.Enrich.Weather`,
  `h.Trips.Expense`, `h.Core.Auth`. Adding a service = one field in one group
  struct + one line in `main.go`. Do not reintroduce flat fields.
- **Use the handler helpers**, not raw `w.Write`: `h.jsonResponse`,
  `h.jsonError`, `h.getUserID`, `h.render`, `h.parseJSON`.
- **Optional integrations must fail soft.** Every enrichment service returns an
  empty result or a graceful 503 when its API key is unset. The app must boot
  and serve with nothing but `SESSION_SECRET` and a Mongo URI. Never add a
  hard dependency on an optional key.
- **Migrations are versioned and idempotent.** Add
  `internal/migrations/mNNN_name.go`, `Register()` from `init()`, next version
  number. They run on every server boot.
- **No frontend build step.** Server-rendered `html/template` + Alpine.js and
  Tailwind via CDN, one `app.css`, one `app.js`. Do not introduce npm, a
  bundler, or a JS framework.
- **Services are concrete structs, not interfaces** (there are only 4 interfaces
  in ~28k lines). This is deliberate. HTTP-calling services take a base URL so
  tests point them at `httptest.Server` — see
  `internal/service/http_services_test.go`. Don't add interfaces just to mock.
- Encrypt sensitive traveler fields via `internal/crypto` (passport, KTN,
  loyalty numbers).

## Traps

- **Integration tests skip silently** without `MONGODB_TEST_URI`. A green local
  `make test` does not mean `internal/repository` or `internal/migrations` was
  exercised.
- **`DestinationHandler` also uses receiver `h`** and has its own
  `scraperService` field. A blind find-replace across `internal/handler` on
  `h.<field>` will corrupt `destination_handler.go`.
- **Never mix fields between browser profiles.** `scraping/profile.rs` bundles
  UA, `navigator.platform`, `Sec-CH-UA*`, `Accept`, and `Accept-Language` into
  one `BrowserProfile` precisely because sampling them independently produced
  contradictions (a Firefox UA with Chrome client hints). Read them only via
  `profile::active()`, never a fresh draw per call site, and never hardcode a
  platform string. Profiles are host-OS filtered on purpose. Tests in
  `profile.rs` and `browser.rs::header_coherence_tests` enforce this.
- **The scraper's `reqwest` feature list is load-bearing, not ergonomic.**
  `http2`, `cookies`, and the decompression features exist because omitting them
  contradicts the Chrome UA it sends (HTTP/1.1-only ALPN, no session cookies,
  `Accept-Encoding: identity`). Don't prune them to slim the build.
- **Browser version is derived, never hardcoded.** `profile::detect_chrome_version`
  reads the installed Chrome and every version-bearing field renders from it.
  Adding a hardcoded version string re-introduces the drift this replaced.
- **Stealth JS goes through `evaluate_on_new_document`, not `evaluate`.** The
  latter runs against the current document after the call resolves, which is too
  late for the page's own document-start scripts. Do not re-inject after
  navigation — re-applying the `Function.prototype.toString` patch over an
  already-patched `toString` is how that trick gets noticed.
- **The three scraper search-URL builders must stay separate.**
  `build_stealth_search_url` (`stealth_flow.rs`), `build_legacy_search_url`
  (`browser_flow.rs`), and `build_search_url` (`fast_path.rs`) differ in query
  encoding and parameters, and each returns different Airbnb results. Merging
  them is a behaviour change. Tests pin the exact URLs.
- **`.cargo/config.toml`'s `-Z threads=16` is nightly-only** and commented out
  for that reason. Enabling it breaks every cargo command on stable.
- **ChromeDriver goes stale** whenever Chrome updates; run
  `rust-scraper/fetch-chromedriver.sh`. Never commit the binary.
- **Scraper extraction is layered** (Airbnb JSON → JSON-LD → regex → DOM). If a
  field goes missing but the scrape succeeds, an extractor in
  `scraping/extract.rs` has gone stale rather than the flow being broken.
- `gofmt` is a hard CI gate; `gosec`/`govulncheck` are advisory. Don't
  "fix" a gosec finding without checking the existing `#nosec` rationale
  comments — e.g. `crypto/sha1` is correct for TOTP.
