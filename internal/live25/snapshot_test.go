package live25

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestSnapshotDeduplicatesEventsAndFiltersReservationsToRoom(t *testing.T) {
	reader := &fakeReader{
		location: Location{ID: 42, Name: "TEST-ROOM"},
		availability: AvailabilityPage{Days: []AvailabilityDay{
			{Entries: []AvailabilityEntry{{EventID: 20}, {EventID: 10}}},
			{Entries: []AvailabilityEntry{{EventID: 20}}},
		}},
		details: map[int]EventDetail{
			10: {Events: []Event{{
				ID: 10, Name: "event 10", Profiles: []Profile{{
					ID: 1,
					Reservations: []Reservation{{
						ID: 100, Spaces: []ReservationSpace{{ID: 42}, {ID: 99}},
					}},
				}},
			}}},
			20: {Events: []Event{{
				ID: 20, Name: "event 20", Profiles: []Profile{{
					ID:           2,
					Reservations: []Reservation{{ID: 200, Spaces: []ReservationSpace{{ID: 99}}}},
				}},
			}}},
		},
	}

	snapshot, err := Snapshot(context.Background(), reader, RoomSnapshotRequest{
		SpaceID: 42,
		Start:   time.Date(2026, time.November, 2, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}
	if reader.detailIDs[0] != 10 || reader.detailIDs[1] != 20 || len(reader.detailIDs) != 2 {
		t.Fatalf("detail IDs = %#v, want sorted unique IDs", reader.detailIDs)
	}
	if len(snapshot.Events) != 1 || snapshot.Events[0].ID != 10 {
		t.Fatalf("events = %#v", snapshot.Events)
	}
	reservation := snapshot.Events[0].Profiles[0].Reservations[0]
	if len(reservation.Spaces) != 1 || reservation.Spaces[0].ID != 42 {
		t.Fatalf("reservation spaces = %#v", reservation.Spaces)
	}
}

func TestSnapshotRefusesIncompleteAvailability(t *testing.T) {
	reader := &fakeReader{
		location:     Location{ID: 42},
		availability: AvailabilityPage{PageCount: 2},
	}
	_, err := Snapshot(context.Background(), reader, RoomSnapshotRequest{
		SpaceID: 42,
		Start:   time.Date(2026, time.November, 2, 0, 0, 0, 0, time.UTC),
	})
	if !errors.Is(err, ErrIncompleteAvailability) {
		t.Fatalf("Snapshot() error = %v, want ErrIncompleteAvailability", err)
	}
	if len(reader.detailIDs) != 0 {
		t.Fatalf("detail reads = %#v, want none", reader.detailIDs)
	}
}

func TestSnapshotIncludesEventReadFailure(t *testing.T) {
	reader := &fakeReader{
		location:     Location{ID: 42},
		availability: AvailabilityPage{Days: []AvailabilityDay{{Entries: []AvailabilityEntry{{EventID: 10}}}}},
		detailError:  errors.New("forbidden"),
	}
	_, err := Snapshot(context.Background(), reader, RoomSnapshotRequest{
		SpaceID: 42,
		Start:   time.Date(2026, time.November, 2, 0, 0, 0, 0, time.UTC),
	})
	if err == nil || !errors.Is(err, reader.detailError) {
		t.Fatalf("Snapshot() error = %v, want detail error", err)
	}
}

func TestSnapshotBuildingsDeduplicatesRoomsAndPreservesRoomErrors(t *testing.T) {
	reader := &fakeReader{
		locationsByBuilding: map[int][]Location{
			7: {{ID: 42, Name: "shared"}, {ID: 43, Name: "broken"}},
			8: {{ID: 42, Name: "shared"}, {ID: 44, Name: "working"}},
		},
		location:     Location{ID: 42, Name: "room"},
		availability: AvailabilityPage{},
	}
	// Make one room fail during its location fetch while allowing the other
	// discovered rooms to remain represented in the bulk result.
	reader.locationErrors = map[int]error{43: errors.New("forbidden")}

	snapshot, err := SnapshotBuildings(context.Background(), reader, []Building{
		{ID: 8, Code: "FB"}, {ID: 7, Code: "SN"},
	}, RoomSnapshotRequest{SpaceID: 1, Start: time.Date(2026, time.November, 2, 0, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatalf("SnapshotBuildings() error = %v", err)
	}
	if len(snapshot.Buildings) != 2 || snapshot.Buildings[0].ID != 7 || len(snapshot.Results) != 3 {
		t.Fatalf("SnapshotBuildings() = %#v", snapshot)
	}
	if got := snapshot.Results[1]; got.Location.ID != 43 || got.Error == "" || got.Snapshot != nil {
		t.Fatalf("failed room result = %#v", got)
	}
}

type fakeReader struct {
	location            Location
	availability        AvailabilityPage
	details             map[int]EventDetail
	detailError         error
	detailIDs           []int
	locationsByBuilding map[int][]Location
	locationErrors      map[int]error
}

func (f *fakeReader) Location(_ context.Context, spaceID int) (Location, error) {
	if err := f.locationErrors[spaceID]; err != nil {
		return Location{}, err
	}
	location := f.location
	location.ID = spaceID
	return location, nil
}

func (f *fakeReader) Availability(context.Context, AvailabilityRequest) (AvailabilityPage, error) {
	return f.availability, nil
}

func (f *fakeReader) EventDetail(_ context.Context, eventID int) (EventDetail, error) {
	f.detailIDs = append(f.detailIDs, eventID)
	if f.detailError != nil {
		return EventDetail{}, f.detailError
	}
	return f.details[eventID], nil
}

func (f *fakeReader) Buildings(context.Context) ([]Building, error) { return nil, nil }

func (f *fakeReader) LocationsForBuilding(_ context.Context, buildingID int) ([]Location, error) {
	return f.locationsByBuilding[buildingID], nil
}
