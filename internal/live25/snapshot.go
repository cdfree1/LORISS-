package live25

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"
)

// ErrIncompleteAvailability means the observed availability endpoint reported
// more than one page. Its paging contract has not yet been verified, so the
// snapshot intentionally fails rather than silently omitting room events.
var ErrIncompleteAvailability = errors.New("live25: availability response is incomplete")

// RoomSnapshotRequest identifies a single 25Live room and discovery window.
type RoomSnapshotRequest struct {
	SpaceID  int
	Start    time.Time
	PageSize int
}

// RoomSnapshot is a read-only view of events discovered for one configured
// room. Reservations in Events are filtered to the requested room; the
// availability view remains available for audit and date-window context.
type RoomSnapshot struct {
	Location     Location
	Availability AvailabilityPage
	Events       []RoomEvent
	CollectedAt  time.Time
}

// BuildingSnapshot is a collection of room snapshots selected by canonical
// 25Live building IDs. Room-level failures are retained in Results so one
// unavailable room does not discard successfully read rooms.
type BuildingSnapshot struct {
	Buildings   []Building
	Results     []RoomSnapshotResult
	CollectedAt time.Time
}

type RoomSnapshotResult struct {
	Location Location
	Snapshot *RoomSnapshot `json:",omitempty"`
	Error    string        `json:",omitempty"`
}

// RoomEvent is a detail event projection constrained to a single room.
type RoomEvent struct {
	ID           int
	ETag         string
	Name         string
	Title        string
	EventLocator string
	Active       bool
	Updated      string
	State        int
	TypeID       int
	Profiles     []RoomProfile
}

type RoomProfile struct {
	ID           int
	Name         string
	Recurrence   OccurrenceDefinition
	Reservations []Reservation
}

// Snapshot reads location metadata and availability, then enriches each unique
// discovered event with its detail response. Detail requests run sequentially
// to avoid making unsupported assumptions about 25Live rate limits.
func Snapshot(ctx context.Context, reader Reader, request RoomSnapshotRequest) (RoomSnapshot, error) {
	if reader == nil {
		return RoomSnapshot{}, fmt.Errorf("%w: reader is required", ErrInvalidRequest)
	}
	location, err := reader.Location(ctx, request.SpaceID)
	if err != nil {
		return RoomSnapshot{}, fmt.Errorf("live25: read location: %w", err)
	}
	availability, err := reader.Availability(ctx, AvailabilityRequest{
		SpaceID:  request.SpaceID,
		Start:    request.Start,
		PageSize: request.PageSize,
	})
	if err != nil {
		return RoomSnapshot{}, fmt.Errorf("live25: read availability: %w", err)
	}
	if availability.PageCount > 1 {
		return RoomSnapshot{}, fmt.Errorf("%w: got %d pages", ErrIncompleteAvailability, availability.PageCount)
	}

	ids := make(map[int]struct{})
	for _, day := range availability.Days {
		for _, entry := range day.Entries {
			if entry.EventID > 0 {
				ids[entry.EventID] = struct{}{}
			}
		}
	}
	eventIDs := make([]int, 0, len(ids))
	for id := range ids {
		eventIDs = append(eventIDs, id)
	}
	sort.Ints(eventIDs)

	snapshot := RoomSnapshot{
		Location:     location,
		Availability: availability,
		CollectedAt:  time.Now().UTC(),
	}
	for _, eventID := range eventIDs {
		detail, err := reader.EventDetail(ctx, eventID)
		if err != nil {
			return RoomSnapshot{}, fmt.Errorf("live25: read event %d: %w", eventID, err)
		}
		snapshot.Events = append(snapshot.Events, roomEvents(detail, request.SpaceID)...)
	}
	return snapshot, nil
}

// SnapshotBuildings discovers locations for the supplied buildings, removes
// duplicate room IDs, then reads every room sequentially. Sequential operation
// is intentional until the 25Live rate limit and bulk-read contract are known.
func SnapshotBuildings(ctx context.Context, reader BuildingReader, buildings []Building, request RoomSnapshotRequest) (BuildingSnapshot, error) {
	if reader == nil {
		return BuildingSnapshot{}, fmt.Errorf("%w: building reader is required", ErrInvalidRequest)
	}
	if len(buildings) == 0 {
		return BuildingSnapshot{}, fmt.Errorf("%w: at least one building is required", ErrInvalidRequest)
	}

	uniqueBuildings := make(map[int]Building)
	for _, building := range buildings {
		if building.ID <= 0 {
			return BuildingSnapshot{}, fmt.Errorf("%w: building ID must be positive", ErrInvalidRequest)
		}
		uniqueBuildings[building.ID] = building
	}
	buildingIDs := make([]int, 0, len(uniqueBuildings))
	for id := range uniqueBuildings {
		buildingIDs = append(buildingIDs, id)
	}
	sort.Ints(buildingIDs)

	snapshot := BuildingSnapshot{CollectedAt: time.Now().UTC()}
	locations := make(map[int]Location)
	for _, id := range buildingIDs {
		building := uniqueBuildings[id]
		snapshot.Buildings = append(snapshot.Buildings, building)
		found, err := reader.LocationsForBuilding(ctx, id)
		if err != nil {
			return BuildingSnapshot{}, fmt.Errorf("live25: list locations for building %d: %w", id, err)
		}
		for _, location := range found {
			if location.ID > 0 {
				locations[location.ID] = location
			}
		}
	}

	locationIDs := make([]int, 0, len(locations))
	for id := range locations {
		locationIDs = append(locationIDs, id)
	}
	sort.Ints(locationIDs)
	for _, id := range locationIDs {
		location := locations[id]
		room, err := Snapshot(ctx, reader, RoomSnapshotRequest{
			SpaceID: id, Start: request.Start, PageSize: request.PageSize,
		})
		result := RoomSnapshotResult{Location: location}
		if err != nil {
			result.Error = err.Error()
		} else {
			result.Snapshot = &room
		}
		snapshot.Results = append(snapshot.Results, result)
	}
	return snapshot, nil
}

func roomEvents(detail EventDetail, spaceID int) []RoomEvent {
	result := make([]RoomEvent, 0, len(detail.Events))
	for _, event := range detail.Events {
		roomEvent := RoomEvent{
			ID: event.ID, ETag: event.ETag, Name: event.Name, Title: event.Title,
			EventLocator: event.EventLocator, Active: event.Active, Updated: event.Updated,
			State: event.State, TypeID: event.TypeID,
		}
		for _, profile := range event.Profiles {
			roomProfile := RoomProfile{ID: profile.ID, Name: profile.Name, Recurrence: profile.Recurrence}
			for _, reservation := range profile.Reservations {
				filtered := reservationForSpace(reservation, spaceID)
				if filtered != nil {
					roomProfile.Reservations = append(roomProfile.Reservations, *filtered)
				}
			}
			if len(roomProfile.Reservations) > 0 {
				roomEvent.Profiles = append(roomEvent.Profiles, roomProfile)
			}
		}
		if len(roomEvent.Profiles) > 0 {
			result = append(result, roomEvent)
		}
	}
	return result
}

func reservationForSpace(reservation Reservation, spaceID int) *Reservation {
	filtered := reservation
	filtered.Spaces = nil
	for _, space := range reservation.Spaces {
		if space.ID == spaceID {
			filtered.Spaces = append(filtered.Spaces, space)
		}
	}
	if len(filtered.Spaces) == 0 {
		return nil
	}
	return &filtered
}
