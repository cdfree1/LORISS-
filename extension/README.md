# LORISS 25Live Access

A minimal Chrome Manifest V3 extension that verifies whether the active Chrome
profile can make one read-only 25Live request. It is intentionally limited to
access verification.

It does **not** fetch schedules, discover SN/FB rooms, normalize or
deduplicate events, synchronize data, write data, use a database, read
cookies, or ask for credentials. Popup state and returned data disappear when
the popup closes.

## Prerequisites

- Chrome or Chromium with Developer mode available.
- Node.js 20+ and npm to build from source.
- A regular 25Live session already signed in at
  `https://25live.collegenet.com/` in the same Chrome profile.

## Build

From the repository root:

```text
cd extension
npm ci
npm run build
```

The unpacked extension is created in `extension/dist`. The generated directory
and `node_modules` are deliberately not committed.

## Install locally in Chrome

1. Open `chrome://extensions`.
2. Enable **Developer mode**.
3. Choose **Load unpacked**.
4. Select the generated `extension/dist` directory.
5. Sign into 25Live normally in the same Chrome profile, then open the LORISS
   popup and choose **Test 25Live Access**.

After changing source files, run `npm run build`, return to
`chrome://extensions`, and use the extension card's reload icon.

## Expected results

- **Access confirmed:** the current browser session successfully read the
  25Live building-list endpoint.
- **Not authorized:** 25Live returned 401/403; sign in or refresh the normal
  25Live session and retry.
- **Malformed response:** the endpoint returned JSON that was not the expected
  building-list shape.
- **Network failure:** Chrome could not reach 25Live.

Returned building data is checked but never displayed, logged, or persisted.

## Deploy from GitHub

Push the source, tests, README, and `package-lock.json`—not `dist/` or
`node_modules/`. GitHub Actions runs the extension's lint and test suite for
changes under `extension/`.

To make a release ZIP after a clean build:

```text
cd extension/dist
zip -r ../loriss-25live-access.zip .
```

The ZIP must contain `manifest.json` at its top level. It can be shared for
unpacked installation or uploaded through the Chrome Web Store developer
dashboard when you are ready to publish. Keep the source repository as the
authoritative artifact; build each release from its tagged commit.

## Verify

```text
cd extension
npm run lint
npm test
```

`npm test` rebuilds the extension and runs unit tests for successful access,
authorization failure, malformed responses, and network failures.

## Implementation note

The extension calls only this read-only endpoint already documented by the
repository's `internal/live25` package:

```text
https://25live.collegenet.com/25live/data/unc/run/micro/building/list.json?caller=pro-BuildingService.getBuildings
```

It uses `fetch` with `credentials: "include"`, allowing the normal active
browser session to authenticate the request. The manifest requests only the
exact 25Live host permission. It does not request extension API permissions or
use `cookies`, `tabs`, `webRequest`, clipboard, debugger, or storage APIs.
