# Development guide

Receipt Studio 0.9.0 is a native Go/MyGo desktop application published by Wizard
Health. Its identifier is `com.wizardhealth.receiptstudio`. Source and downloads
share the public [GitHub repository](https://github.com/WizardComputer/receipt-studio).
There is no separate website.

## Running locally

Use the Go toolchain specified in `go.mod`. MyGo is pinned to 0.9.1 and
ReceiptPrinterEncoder to 4.0.1. Node.js 22+ is needed for encoder tests and vendor
maintenance, not for the installed application.

```sh
npm ci --ignore-scripts
bash scripts/dev-native.sh --listen 127.0.0.1:17842 --data-dir "$PWD/.native-dev"
```

The script uses `-tags desktop` and disables autostart. Without the desktop tag,
`go run ./cmd/receipt-studio` starts the headless API. Do not run two agents against the same data
directory; a directory lock enforces this. The default installed port is 17840;
the development example uses 17842 and separate data. All listeners are loopback-only.

## Architecture

```text
cmd/receipt-studio/  executable entry point
internal/app/        startup, shutdown, flags and single-instance locking
internal/agent/      admission, idempotency, administration and print worker
internal/api/        authenticated public HTTP routes
internal/storage/    SQLite schema, transactions, queries and snapshots
internal/receipt/    Goja/RPE encoder, document editing, layout and PNG rendering
internal/printer/    LAN transport, USB queues and isolated driver subprocess
internal/desktop/    MyGo screens, editor, tray and platform integration
internal/model/      shared receipt/job/configuration types and action errors
internal/i18n/       UI translations
resources.go        embedded runtime assets and version metadata only
```

`app` composes the agent, HTTP adapter and desktop. The HTTP adapter depends on
its narrow `Service` interface; desktop calls the agent directly. The agent uses
storage, receipt and printer packages. These packages share model types and do
not import the agent or UI. SQL lives in storage; integration tests may access
temporary databases directly to simulate interrupted jobs and damaged storage.
No database schema, template format, HTTP contract or printer protocol changes
are implied by this package layout.

The single root Go file is the resource package: `go:embed` cannot traverse `..`,
so it embeds the existing JS sources, translations, tray image and `mygo.json`
without duplicate copies or generated version files. The executable target in
`mygo.json` is `./cmd/receipt-studio`.


- MyGo `ui.View` draws the native interface; there is no WebView, web administration
  or internal HTTP adapter. Screens call typed Go administration operations.
- Templates describe flow-based receipts. Moving an element vertically changes
  order; dynamic tables push subsequent elements down. Horizontal percentages
  snap to font columns, rather than introducing arbitrary page-mode positioning.
- `src/core.js` validates templates and data and calls the bundled RPE encoder.
  Goja runs a fresh runtime per operation with a shared compiled program and a
  deadline. JSON is passed directly to the runtime; binary commands use owned buffers.
- `src/escpos-preview.js` decodes the generated commands. The editor draws the
  resulting layout as tiled SVG. Its QR symbol is illustrative; exported PNGs
  render the actual QR content.
- PNG rendering uses bounded high-resolution bands, downsamples them into the final
  paper image and compresses it. The final image and an open preview's decoded
  bitmap still occupy memory. Closing a modal releases its bitmap references.
- SQLite stores configuration, immutable template revisions, job metadata,
  original receipt snapshots/commands, cached PNGs and diagnostics. Job queries
  filter and paginate in SQL; the worker and details views do not eagerly load PNGs.
- The worker claims jobs durably before sending. Duplicate IDs do not enqueue the
  same request twice; conflicting reuse returns an error. Uncertain delivery is
  never retried automatically. The API caller supplies monetary calculations.

The initial/minimum window size is 1400 × 760 logical pixels, capped at the primary
screen's work area when created. BS/EN UI text uses the translation helpers;
user content and API identifiers remain unchanged. Seven clicks on the sidebar
version toggle the diagnostics screen. Diagnostic records expire after 15 days;
print history is retained.

## Storage and migrations

The eight tables are `settings`, `printers`, `templates`, `template_versions`,
`jobs`, `job_receipts`, `job_pngs` and `diagnostics`. See [AGENT.md](AGENT.md) for their
fields and purpose. PNGs and command bytes are binary database values; Base64 is
used only when a JSON API response requests it.

SQLite schema version 1 is the baseline. Only an empty, unversioned database is
initialized. Unsupported versions/fingerprints and an existing database missing
its API key fail without deleting data. There is no development reset or legacy
configuration import. Future schema changes require ordered transactional
migrations, preservation/rollback tests and an increment of `PRAGMA user_version`.
Application versions, API version 1 and template revisions are independent.

Installed data directories are `~/Library/Application Support/ReceiptStudio` on
macOS and `%APPDATA%\ReceiptStudio` on Windows. Keep them stable across updates.
Never commit databases, WAL/SHM files, receipt payloads, API keys or signing keys.

## Tests

```sh
go test ./...
go test -tags desktop ./...
go test -race -tags desktop ./...
npm test
```

Frozen fixtures in `testdata/encoder.json` check command-byte and layout parity.
PNG fixtures check decoded pixels, including renderer band boundaries. Native UI
tests use MyGo's software tester. Transport tests use local simulators, not physical
printers. Optional tests below are skipped during ordinary test runs.

Pixel hashes preserve the original full-canvas renderer's output. For six text
fixtures, `png-pixels-unfused.json` also records its exact AMD64 output: vector-font
interpolation without ARM64 fused multiply-add changes ten pixels by 1/255.
Both variants were checked against the original and banded renderers. Tests accept
only these frozen hashes; they do not skip platforms or apply a pixel tolerance.

Render native UI screenshots:

```sh
RECEIPT_NATIVE_SCREENSHOTS=/private/tmp/receipt-native-ui \
  go test -tags desktop -run TestNativeScreens ./internal/desktop
```

Run PNG-generation and full HTTP round-trip measurements for receipts with
10, 50, 100, 150, 200, 300, 400 and 500 items (requires Python 3):

```sh
RECEIPT_BENCHMARK_OUTPUT=/private/tmp/receipt-png-benchmark \
  go test -run '^TestReceiptPNGPerformance$' -count=1 -v -timeout=15m ./internal/agent
```

Then measure decoded-image/modal retention using those generated fixtures:

```sh
RECEIPT_MODAL_MEMORY_INPUT=/private/tmp/receipt-png-benchmark \
RECEIPT_MODAL_MEMORY_OUTPUT=/private/tmp/receipt-modal-memory \
  go test -tags desktop -run '^TestNativeReceiptModalMemory$' -count=1 -v -timeout=15m ./internal/desktop
```

Keep reports outside the source tree. They may contain local paths or machine
metadata. Measurements are not production performance guarantees.

## Maintaining the encoder

`scripts/sync-rpe.mjs` copies the locked encoder and its notices from `node_modules`.
`scripts/update-encoder-fixtures.mjs` changes expected output: run it only for an
intentional encoder change, not to hide a regression. Template exports use
`receipt-studio-template` package version 2; the inner design schema is version 1.
`scripts/prepare-demo-jobs.mjs` can prepare isolated demonstration history without
sending data to a physical printer.

## Packaging

`mygo.json` is the application-version source for native branding, health responses
and build artifacts. `package.json` and its lockfile mirror the version;
`TestApplicationVersion` checks consistency. The first tag is `v0.9.0`.

Use **Actions → Build test installers → Run workflow** to explicitly build:

| Runner | Target | Installer |
| --- | --- | --- |
| `windows-2025` | Windows x64 | `receipt-studio-setup-0.9.0-amd64.exe` |
| `macos-15` | macOS Apple Silicon | `receipt-studio-0.9.0-arm64.dmg` |

Each job runs JS and Go/native tests before packaging. Installers are attached to
the workflow run as artifacts for 14 days; a missing installer fails the job.
The workflow has read-only repository permissions and does not run on pushes/tags
or publish Releases. MyGo downloads NSIS on Windows when needed, so contributors
do not need NSIS on their Mac to use the CI workflow.

The app icon master is `packaging/assets/app-icon.png`: a receipt printer with
Wizard Health's green wizard hat. `app-icon.json` records its generation brief.
Run `go run ./tools/icons` from the repository root to regenerate the macOS iconset
and multi-resolution Windows `app-icon.ico`; this creates image assets only.
MyGo consumes the 1024-pixel PNG named in `mygo.json`. The monochrome menu-bar
tray asset remains separate from the application icon.

Local packaging is available only when a build is explicitly requested:

```sh
bash scripts/build-macos.sh --check
bash scripts/build-windows.sh --check
GOARCH=arm64 bash scripts/build-macos.sh
bash scripts/build-windows.sh
```

`--check` verifies tool availability without building. Local macOS packaging needs
Go, `codesign` and `hdiutil`; local Windows Setup builds on a Mac also need NSIS.
Output stays under ignored `dist/mygo`. The Windows installer uses a per-user
installation in `%LOCALAPPDATA%\Programs\Receipt Studio`. Its uninstall hook removes
the app's startup registry entry and preserves the database. On macOS, disable
autostart before removing the application; the user data directory remains.

## Release work remaining

0.9.0 has not been tested in production. No production signing, real automatic
update integration or release-publishing workflow is configured yet.

1. Configure macOS Developer ID signing/notarization and Windows code signing.
2. Generate and securely back up the separate MyGo updater key pair; keep the
   private key in CI secrets and embed only its public key.
3. Configure signed packages/manifests for `WizardComputer/receipt-studio` Releases.
4. Replace the update UI simulation with `mygo.Updater` and SQLite-backed settings.
5. Coordinate updates with active printing, queued jobs and unsaved editor changes.
6. Publish only approved packages after clean-machine installation/uninstallation
   and A-to-B update tests that preserve keys, templates, jobs and settings.

A Git tag is a source version, not a published installer or working update feed.
GitHub Actions artifacts are temporary test downloads; approved installers and
signed manifests belong in GitHub Releases. No GitHub token belongs in an installed
agent. For manual installation over an existing app, finish printing and close the
agent first. Keep temporary builds out of source commits.

## Known limitations

- Profiles are generic; preview fonts and printer firmware can differ. Validate
  physical output for each printer model and paper width.
- One sender serves all printers, and expensive receipt generation still shares
  the admission lock. Slow devices or large jobs can delay other work.
- The jobs list retains loaded rows; it is not virtualized. Receipt modal decoding
  runs synchronously and opens the full bitmap. Very long histories/images can
  therefore affect UI responsiveness and memory use.
- Print history has no retention limit. Monitor disk usage and back up the database
  consistently; a full backup/restore UI is not implemented.
- The binary receipt endpoint avoids Base64 expansion but loads the compressed PNG
  into memory. Request size is capped at 1 MiB and response writes at 45 seconds.
- Send large business identifiers as JSON strings. Generic numeric JSON comparison
  is not intended to preserve arbitrary-precision integer identifiers.

References: [MyGo distribution](https://mygo.run/docs/distribution),
[MyGo updates](https://mygo.run/docs/updates).
