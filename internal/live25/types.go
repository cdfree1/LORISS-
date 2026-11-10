package live25

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const availabilityCaller = "pro-AvailService.getData"

// Location is the stable 25Live identity and metadata of a bookable space.
type Location struct {
	ID          int
	Name        string
	FormalName  string
	BuildingID  int
	Building    string
	Status      string
	MaxCapacity int
}

// Building is a 25Live building identity used to select an explicit room
// scope. Code is the short campus code such as SN or FB when provided by the
// configured 25Live instance.
type Building struct {
	ID   int
	Code string
	Name string
}

// AvailabilityRequest selects the daily room-availability view beginning at
// Start. The endpoint reports day-grouped entries; callers fetch EventDetail
// for complete reservation timestamps and recurrence data.
type AvailabilityRequest struct {
	SpaceID  int
	Start    time.Time
	PageSize int
}

// AvailabilityPage is one 25Live daily-availability response.
type AvailabilityPage struct {
	PageCount  int
	LastID     int
	LastUpdate string
	Days       []AvailabilityDay
}

type AvailabilityDay struct {
	Date    string
	Entries []AvailabilityEntry
}

// AvailabilityEntry is a lightweight event observation. Times are kept as
// strings because the observed endpoint can return either strings or numeric
// time-of-day values. EventDetail supplies authoritative timestamps.
type AvailabilityEntry struct {
	EventID     int
	SecondaryID int
	Name        string
	State       int
	Start       string
	End         string
	PreStart    string
	PostEnd     string
	EventTypeID int
}

// EventDetail contains a complete, read-only event projection. The adapter
// deliberately omits expanded contact records and other personal data that
// are not needed for room synchronization.
type EventDetail struct {
	ID      int
	Updated string
	Events  []Event
	Spaces  []ExpandedSpace
	States  []EventState
}

type Event struct {
	ID           int
	ETag         string
	Name         string
	Title        string
	EventLocator string
	Priority     int
	Active       bool
	Updated      string
	StartDate    string
	EndDate      string
	State        int
	TypeID       int
	ParentID     int
	Profiles     []Profile
}

type Profile struct {
	ID            int
	Name          string
	ExpectedCount int
	Registered    int
	Recurrence    OccurrenceDefinition
	Reservations  []Reservation
}

type OccurrenceDefinition struct {
	RecurrenceTypeID int
	ProfileCode      string
	InitialStart     string
	InitialEnd       string
}

type Reservation struct {
	ID         int
	State      int
	Start      string
	End        string
	EventStart string
	EventEnd   string
	Spaces     []ReservationSpace
}

type ReservationSpace struct {
	ID         int
	Shared     bool
	Attendance int
	Rating     int
}

type ExpandedSpace struct {
	ID          int
	ETag        string
	Name        string
	FormalName  string
	MaxCapacity int
}

type EventState struct {
	ID   int
	Name string
}

// Location returns metadata for an explicitly configured room ID.
func (c *Client) Location(ctx context.Context, spaceID int) (Location, error) {
	if spaceID <= 0 {
		return Location{}, fmt.Errorf("%w: space ID must be positive", ErrInvalidRequest)
	}
	query := url.Values{
		"space_id": {strconv.Itoa(spaceID)},
		"scope":    {"minimal"},
		"caller":   {"pro-SpaceService.getSpaceMinimal"},
	}
	var response locationResponse
	if err := c.get(ctx, "space.json", query, &response); err != nil {
		return Location{}, err
	}
	if len(response.Spaces.Space) != 1 {
		return Location{}, fmt.Errorf("live25: expected one location for space ID %d, got %d", spaceID, len(response.Spaces.Space))
	}
	return response.Spaces.Space[0].location(), nil
}

// Buildings returns the buildings visible to the authenticated local user.
func (c *Client) Buildings(ctx context.Context) ([]Building, error) {
	query := url.Values{"caller": {"pro-BuildingService.getBuildings"}}
	var response buildingsResponse
	if err := c.get(ctx, "micro/building/list.json", query, &response); err != nil {
		return nil, err
	}
	buildings := make([]Building, 0, len(response.Content.Data.Items))
	for _, item := range response.Content.Data.Items {
		buildings = append(buildings, Building{ID: item.ID, Code: item.Code, Name: item.Name})
	}
	return buildings, nil
}

// LocationsForBuilding returns all locations returned by the observed 25Live
// building lookup. The endpoint's paging contract is not documented; callers
// should stop and extend this adapter if that contract changes rather than
// silently assuming this is an exhaustive list.
func (c *Client) LocationsForBuilding(ctx context.Context, buildingID int) ([]Location, error) {
	if buildingID <= 0 {
		return nil, fmt.Errorf("%w: building ID must be positive", ErrInvalidRequest)
	}
	query := url.Values{
		"building_id": {strconv.Itoa(buildingID)},
		"caller":      {"pro-SpaceService.getSpacesByPartId"},
	}
	var response locationsResponse
	if err := c.get(ctx, "spaces.json", query, &response); err != nil {
		return nil, err
	}
	locations := make([]Location, 0, len(response.Spaces.Space))
	for _, item := range response.Spaces.Space {
		locations = append(locations, item.location())
	}
	return locations, nil
}

// Availability returns the room's daily availability observations. The
// observed client uses a page size of 100 rows; larger requests are rejected
// locally rather than silently changed.
func (c *Client) Availability(ctx context.Context, request AvailabilityRequest) (AvailabilityPage, error) {
	if request.SpaceID <= 0 {
		return AvailabilityPage{}, fmt.Errorf("%w: space ID must be positive", ErrInvalidRequest)
	}
	if request.Start.IsZero() {
		return AvailabilityPage{}, fmt.Errorf("%w: availability start is required", ErrInvalidRequest)
	}
	if request.PageSize == 0 {
		request.PageSize = 100
	}
	if request.PageSize < 1 || request.PageSize > 100 {
		return AvailabilityPage{}, fmt.Errorf("%w: page size must be between 1 and 100", ErrInvalidRequest)
	}
	query := url.Values{
		"obj_cache_accl": {"0"},
		"start_dt":       {request.Start.Format("2006-01-02T15:04:05")},
		"comptype":       {"availability_daily"},
		"compsubject":    {"location"},
		"page_size":      {strconv.Itoa(request.PageSize)},
		"space_id":       {strconv.Itoa(request.SpaceID)},
		"include":        {"closed blackouts pending related empty"},
		"caller":         {availabilityCaller},
	}
	var response availabilityResponse
	if err := c.get(ctx, "availability/availabilitydata.json", query, &response); err != nil {
		return AvailabilityPage{}, err
	}
	page := AvailabilityPage{PageCount: response.PageCount, LastID: response.LastID, LastUpdate: response.LastUpdate}
	for _, subject := range response.Subjects {
		day := AvailabilityDay{Date: subject.Date}
		for _, item := range subject.Items {
			day.Entries = append(day.Entries, AvailabilityEntry{
				EventID: item.ID, SecondaryID: item.SecondaryID, Name: item.Name,
				State: item.State, Start: item.Start.String(), End: item.End.String(),
				PreStart: item.PreStart.String(), PostEnd: item.PostEnd.String(), EventTypeID: item.EventTypeID,
			})
		}
		page.Days = append(page.Days, day)
	}
	return page, nil
}

// EventDetail fetches the complete detail view and reservation occurrences for
// one event ID. It does not request or return expanded contact information.
func (c *Client) EventDetail(ctx context.Context, eventID int) (EventDetail, error) {
	if eventID <= 0 {
		return EventDetail{}, fmt.Errorf("%w: event ID must be positive", ErrInvalidRequest)
	}
	query := url.Values{
		"caller":  {"pro-EventMicroService.getEventDetailById"},
		"expand":  {"T"},
		"include": {"all"},
	}
	var response eventDetailResponse
	if err := c.get(ctx, fmt.Sprintf("micro/event/%d/detail.json", eventID), query, &response); err != nil {
		return EventDetail{}, err
	}
	return response.toEventDetail(), nil
}

type locationResponse struct {
	Spaces struct {
		Space spaceList `json:"space"`
	} `json:"spaces"`
}

type locationsResponse struct {
	Spaces struct {
		Space spaceList `json:"space"`
	} `json:"spaces"`
}

type spaceWire struct {
	ID           int    `json:"space_id"`
	Name         string `json:"space_name"`
	FormalName   string `json:"formal_name"`
	BuildingID   int    `json:"building_id"`
	BuildingName string `json:"building_name"`
	Status       string `json:"status"`
	MaxCapacity  int    `json:"max_capacity"`
}

func (s spaceWire) location() Location {
	return Location{
		ID: s.ID, Name: s.Name, FormalName: s.FormalName, BuildingID: s.BuildingID,
		Building: s.BuildingName, Status: s.Status, MaxCapacity: s.MaxCapacity,
	}
}

// spaceList handles the singleton shape returned by space.json and the array
// shape expected from a building-wide spaces lookup.
type spaceList []spaceWire

func (s *spaceList) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		*s = nil
		return nil
	}
	var many []spaceWire
	if err := json.Unmarshal(data, &many); err == nil {
		*s = many
		return nil
	}
	var one spaceWire
	if err := json.Unmarshal(data, &one); err != nil {
		return err
	}
	*s = []spaceWire{one}
	return nil
}

type buildingsResponse struct {
	Content struct {
		Data struct {
			Items []struct {
				ID   int    `json:"id"`
				Code string `json:"buildingCode"`
				Name string `json:"buildingName"`
			} `json:"items"`
		} `json:"data"`
	} `json:"content"`
}

type availabilityResponse struct {
	LastID     int                   `json:"last_id"`
	LastUpdate string                `json:"lastupdate"`
	PageCount  int                   `json:"page_count"`
	Subjects   []availabilitySubject `json:"subjects"`
}

type availabilitySubject struct {
	Date  string             `json:"item_date"`
	Items []availabilityItem `json:"items"`
}

type availabilityItem struct {
	ID          int            `json:"itemId"`
	SecondaryID int            `json:"itemId2"`
	Name        string         `json:"itemName"`
	State       int            `json:"cur_event_state"`
	Start       flexibleString `json:"start"`
	End         flexibleString `json:"end"`
	PreStart    flexibleString `json:"pre_start"`
	PostEnd     flexibleString `json:"post_end"`
	EventTypeID int            `json:"event_type_id"`
}

type flexibleString string

func (s *flexibleString) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		*s = ""
		return nil
	}
	var value string
	if err := json.Unmarshal(data, &value); err == nil {
		*s = flexibleString(value)
		return nil
	}
	var number json.Number
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.UseNumber()
	if err := decoder.Decode(&number); err != nil {
		return err
	}
	*s = flexibleString(number.String())
	return nil
}

func (s flexibleString) String() string { return string(s) }
