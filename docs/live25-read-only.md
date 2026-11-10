# Read-only 25Live adapter

`internal/live25` provides a small, read-only client for the 25Live data
responses observed in the UNC web application. It is deliberately not a
browser scraper: it makes JSON HTTP requests, has no UI automation, and does
not contain create, update, or cancellation operations.

The observed application endpoints are not yet confirmed as a supported
Series25 WebServices contract. Treat the adapter as an integration experiment
until UNC confirms that a supported service identity may call them. During the
current local phase, an authorized user may manually provision their own
session through an approved local secret mechanism. Do not configure it with a
password, a HAR file, browser automation, or session extraction.

## Reader contract

```go
type Reader interface {
    Location(context.Context, int) (Location, error)
    Availability(context.Context, AvailabilityRequest) (AvailabilityPage, error)
    EventDetail(context.Context, int) (EventDetail, error)
}
```

`Client` receives an HTTP client from the composition root. That client is the
only place an approved authentication mechanism may be configured. The adapter
does not read secrets from environment variables and never persists them.

For the local user-session phase, `NewSessionHTTPClient` wraps an HTTP client
with a `SessionSource`. Its transport attaches a `Cookie` header only to the
configured 25Live authority and rejects requests to every other host. It does
not implement browser login, cookie extraction, session refresh, or a cookie
jar. Session clients require HTTPS, except for loopback `httptest` use.

`NewKeychainSession` is the supplied macOS `SessionSource`. It reads a Generic
Password item using `security find-generic-password`; it never creates or
updates that item. Use Keychain Access to create the item locally with service
`edu.unc.loriss.live25.session`, a local account name of your choice, and the
complete HTTP Cookie-header value as the password. Do not place that value in a
shell command, a text file, a `.env` file, or this repository.

The session is cached only in memory after the first successful Keychain read,
so one room snapshot produces at most one Keychain access prompt. Restart the
local command after replacing an expired Keychain item.

Run a read-only connectivity check with:

```text
go run ./cmd/live25-check -keychain-account <your-local-account> -space-id <25live-room-id>
```

The command outputs only the selected room metadata. A 401/403 response means
the local session is unavailable, expired, or lacks permission; it does not
print the stored session value.

## Room snapshots

Run a full read-only room snapshot with:

```text
go run ./cmd/live25-snapshot -keychain-account <your-local-account> -space-id <25live-room-id>
```

Use `-start YYYY-MM-DD` to change the availability start date. The command
lists availability, de-duplicates its event IDs, fetches each event detail,
and writes formatted JSON to standard output. It retains only reservations for
the requested room; it does not emit expanded contact records or descriptions.

The observed availability response has no confirmed multi-page retrieval
contract. The command fails instead of producing a partial snapshot when
25Live reports more than one page.

## Sitterson and Brooks building snapshots

Run a multi-room read-only snapshot for the 25Live building codes `SN` and
`FB` (Sitterson and Brooks):

```text
go run ./cmd/live25-buildings-snapshot -keychain-account <your-local-account>
```

Use `-building-codes` to select another comma-separated building-code set.
The command first discovers canonical building IDs and their rooms, removes
duplicate room IDs, then runs the same conservative room snapshot for each
location. It emits a result per room, so a permission or availability failure
for one room does not discard the successful room results. It uses the same
roughly one-month availability window beginning at `-start`; it does not yet
perform six-month horizon iteration.

The default base URL is the observed UNC run path:

```text
https://25live.collegenet.com/25live/data/unc/run
```

Deployment must use HTTPS. HTTP is accepted by the constructor solely to make
local `httptest` verification possible.

## Read flow

1. Configure a canonical room ID obtained from 25Live; do not select rooms by
   a title substring.
2. Call `Location` to validate the configured identity and gather room
   metadata.
3. Call `Availability` for the room and date window. The response is a
   day-grouped discovery view, not the authoritative schedule model.
4. Deduplicate the discovered event IDs and call `EventDetail` for each one.
   Its reservation records carry absolute reservation and event date-times,
   recurrence-profile information, and reserved room IDs.

The current adapter returns timestamps exactly as supplied by 25Live. A future
normalization layer must parse those values with the confirmed provider
timezone rules before they are used for sync decisions.

## Data minimization

The detail response can contain organizer/contact records. The public read
model intentionally omits expanded contacts, email addresses, descriptions,
and other fields not required for room-event discovery. Consumers should avoid
logging event names or titles by default.

## Current limits

- Local user sessions are manually provisioned and expire without refresh.
- No browser login, automation, cookie extraction, or extension is implemented.
- No write methods are implemented.
- The state-number semantics and cancellation behavior require verification
  from UNC/CollegeNET before they influence reconciliation.
- A complete listing/pagination contract has not yet been verified. A poller
  must not infer deletion from a partial availability response.
