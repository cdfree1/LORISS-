# LORISS

LORISS centralizes scheduling data from UNC 25Live and predefined Google
Calendars. The immediate priority is reliable event discovery and a clear,
shared representation of that data. Synchronization, deduplication behavior,
and downstream product decisions will be defined separately.

## Authorization and data sources

- **Google Calendar:** Authorize with Google OAuth. The design should leave
  room to replace or supplement OAuth with a service account if that becomes
  the better operational model.
- **25Live:** Establish a 25Live session using the user's browser session.
  The goal is to use the already established 25Live JSON-reading methods to
  retrieve events without assuming a final implementation mechanism.

## Initial discovery scope

- Fetch 25Live events for rooms in the `SN` and `FB` buildings over a chosen
  timeframe.
- Fetch events from configured Google Calendars over the relevant timeframe.
- Preserve provider identifiers and enough source data to interpret events
  consistently.

## Shared event model

Provider-specific records should be normalized into a central `Event` datatype
with standard fields for:

- name
- time and date
- repetition or recurrence information
- location
- 25Live event ID
- Google Calendar event ID

Events will later be deduplicated by an algorithm defined separately. Do not
invent or couple implementation work to that algorithm before it is specified.

## Engineering direction

The technology stack is under review. Choose the framework and implementation
approach that best fits the revised project requirements rather than treating
the previous stack or architecture as fixed. Keep the immediate work focused
on the 25Live connection, event discovery, Google Calendar ingestion, and the
general project scope.
