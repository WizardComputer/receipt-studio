# Receipt Studio

**A native receipt designer and local printing agent by Wizard Health.**

Design receipts visually, connect a LAN or USB printer, and send receipt data from
any application through a local HTTP API. Receipt Studio generates ESC/POS,
StarPRNT or Star Line commands without rasterizing the entire receipt.

> **0.9.0 — early testing release.** Receipt Studio has not yet been validated in
> production. Automatic updates and distribution signing are still being prepared.

[Downloads](https://github.com/WizardComputer/receipt-studio/releases) ·
[API reference](AGENT.md) · [Development guide](NATIVE.md) ·
[Report an issue](https://github.com/WizardComputer/receipt-studio/issues)

## Features

- Drag-and-drop receipt editor with named elements, resizing, formatting and undo.
- Text, dynamic tables, QR codes and logos, with JSON data bindings such as `{{shop}}`.
- Reusable templates with saved example data, import/export and version history.
- LAN printing over TCP and USB printing through the operating system's print queue.
- Print-job history with filters, payload details, receipt previews and confirmed reprints.
- Optional Base64 PNG responses and a separate binary receipt-download endpoint.
- Local SQLite storage and API-key authentication.
- Native Go/MyGo interface in English and Bosnian, without a WebView.

## Download and install

Installers will be available from [GitHub Releases](https://github.com/WizardComputer/receipt-studio/releases).
There is no separate download website. A source tag alone does not include an installer;
if a release has no installer attached, use the development instructions below.

| Platform | Package | Installation |
| --- | --- | --- |
| Windows x64 | Setup `.exe` | Run Setup and launch Receipt Studio from the Start menu. |
| macOS Apple Silicon | `.dmg` | Drag Receipt Studio to Applications, eject the disk image, then launch the installed app. |

Linux and Intel Mac installers are not currently part of the distribution targets.
Current test packages are not signed for public distribution.

## Getting started

1. Create a template in **Templates**, or import one from another agent.
2. Add text, tables or graphics, then supply example JSON in the editor's **Data** tab.
3. Add a printer in **Printers** and assign a template. For USB, install the printer
   in your operating system first and select its queue in Receipt Studio.
4. Send a test receipt and check the physical output.
5. Open **API connection** to copy your API key and a request tailored to your printer.

The calling application supplies the receipt's items, totals and formatted values.
Receipt Studio renders them using the template assigned to the selected printer.
Template exports include the design and its saved example data; they exclude API
keys, printer connections and print history.

## API example

The agent listens on `127.0.0.1:17840` by default. Requests must originate from the
same computer, including a browser application running on that computer. A remote
backend cannot reach the user's localhost directly.

Set `API_KEY` to the value shown in the app. With a default printer configured and
the starter template assigned:

```sh
curl --fail-with-body http://127.0.0.1:17840/v1/print-jobs \
  -H "Authorization: Bearer $API_KEY" \
  -H "Content-Type: application/json" \
  --data '{
    "version": 1,
    "job_id": "invoice-42-print-1",
    "data": {
      "shop": "Sample Store",
      "address": "Sample City",
      "number": "42",
      "date": "2026-11-20 10:00",
      "items": [{"name": "Coffee", "qty": "1", "price": "3.00"}],
      "total": "3.00"
    }
  }'
```

This request queues a real print job. Use a unique string `job_id` for each intended
print; retry the same ID and payload to avoid queuing it twice. Send invoice IDs as
strings too. An accepted job normally returns HTTP `202`; acceptance does not
confirm that paper was printed.

| Endpoint | Purpose |
| --- | --- |
| `GET /v1/health` | Agent status |
| `POST /v1/print-jobs` | Submit receipt data |
| `GET /v1/print-jobs/{id}` | Read job status |
| `GET /v1/print-jobs/{id}/receipt` | Download the receipt as `image/png` |

All routes require the API key. Add `"return_png": true` to a print request to
receive a Base64 PNG alongside the job response, or use the download endpoint
later. See the [API reference](AGENT.md) (Bosnian) for request fields, responses,
errors and duplicate handling.

## Printer compatibility

Receipt Studio uses [ReceiptPrinterEncoder](https://github.com/NielsLeenheer/ReceiptPrinterEncoder)
to produce native printer commands. Generic 80 mm and 58 mm profiles are included.
Actual support depends on the printer's command language, code pages and features.

Text and tables remain native commands. Logos and raster QR elements may use
individual bitmap commands. The preview follows the encoded layout, but printer
fonts, firmware and cutting behavior can differ from the on-screen rendering.
Always check a physical test receipt for your printer and paper width.

## Data and privacy

Settings, API keys, printers, templates, template revisions, print jobs and receipt
images stay in a local SQLite database outside the application directory. Print
history is retained; diagnostic records are removed after 15 days. Job details
include the submitted payload, so treat the data directory and backups accordingly.

The public HTTP API is restricted to loopback. Administration is available only
through the native interface. A `sent` status means bytes were delivered to the
transport; it is not a physical-print confirmation. Uncertain sends are not
reprinted automatically.

## Development

Requirements: Go as specified in `go.mod` (currently 1.27.3), and Node.js 22+ for
JavaScript tests. Node.js is not needed to run the packaged application.

```sh
git clone https://github.com/WizardComputer/receipt-studio.git
cd receipt-studio
npm ci --ignore-scripts
bash scripts/dev-native.sh --listen 127.0.0.1:17842 --data-dir "$PWD/.native-dev"
```

The development script disables autostart registration. On Windows, use Git Bash,
or run this directly in PowerShell:

```powershell
go run -tags desktop ./cmd/receipt-studio --setup-autostart=false --listen 127.0.0.1:17842 --data-dir .native-dev
```

Run the tests:

```sh
go test ./...
go test -tags desktop ./...
npm test
```

The manual **Build test installers** GitHub Actions workflow tests and packages
Windows and macOS builds. It does not run on pushes or publish Releases.
See [NATIVE.md](NATIVE.md) for architecture, packaging and optional performance tests.

## Feedback and contributions

Use [GitHub Issues](https://github.com/WizardComputer/receipt-studio/issues) for bugs
and feature requests. Include the app version, operating system, printer model,
connection type and reproduction steps. Replace real receipt data with a minimal
example; do not attach API keys, database files or customer information.

Discuss substantial changes in an issue before opening a pull request. Keep encoder
fixtures intact unless the expected output is intentionally changing.

## Licensing

Receipt Studio is licensed under the [MIT License](LICENSE).
Copyright © 2026 Wizard Health. Third-party components retain their own licenses;
bundled encoder notices are in [src/vendor](src/vendor).
