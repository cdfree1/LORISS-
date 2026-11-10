package live25

type eventDetailResponse struct {
	Content struct {
		ID      int    `json:"id"`
		Updated string `json:"updated"`
		Data    struct {
			Items []eventDetailEvent `json:"items"`
		} `json:"data"`
		ExpandedInfo struct {
			Spaces []expandedSpace `json:"spaces"`
			States []expandedState `json:"states"`
		} `json:"expandedInfo"`
	} `json:"content"`
}

type eventDetailEvent struct {
	ID           int    `json:"id"`
	ETag         string `json:"etag"`
	Name         string `json:"name"`
	Title        string `json:"title"`
	EventLocator string `json:"eventLocator"`
	Priority     int    `json:"priority"`
	Active       bool   `json:"isActive"`
	Updated      string `json:"updated"`
	Dates        struct {
		Start string `json:"startDate"`
		End   string `json:"endDate"`
	} `json:"dates"`
	Context struct {
		State    int `json:"state"`
		TypeID   int `json:"typeId"`
		ParentID int `json:"parentId"`
	} `json:"context"`
	Profiles []eventProfile `json:"profiles"`
}

type eventProfile struct {
	ID             int    `json:"profileId"`
	Name           string `json:"name"`
	ExpectedCount  int    `json:"expectedCount"`
	Registered     int    `json:"registeredCount"`
	OccurrenceDefn struct {
		RecurrenceTypeID int    `json:"recTypeId"`
		ProfileCode      string `json:"profileCode"`
		InitialStart     string `json:"initStartDt"`
		InitialEnd       string `json:"initEndDt"`
	} `json:"occurrenceDefn"`
	Reservations []eventReservation `json:"reservations"`
}

type eventReservation struct {
	ID         int    `json:"rsrvId"`
	State      int    `json:"state"`
	Start      string `json:"rsrvStartDt"`
	End        string `json:"rsrvEndDt"`
	EventStart string `json:"evStartDt"`
	EventEnd   string `json:"evEndDt"`
	Spaces     []struct {
		Reserved []struct {
			ID         int  `json:"spaceId"`
			Shared     bool `json:"share"`
			Attendance int  `json:"attendance"`
			Rating     int  `json:"rating"`
		} `json:"reserved"`
	} `json:"spaces"`
}

type expandedSpace struct {
	ID          int    `json:"spaceId"`
	ETag        string `json:"etag"`
	Name        string `json:"spaceName"`
	FormalName  string `json:"spaceFormalName"`
	MaxCapacity int    `json:"maxCapacity"`
}

type expandedState struct {
	ID   int    `json:"state"`
	Name string `json:"stateName"`
}

func (response eventDetailResponse) toEventDetail() EventDetail {
	detail := EventDetail{
		ID:      response.Content.ID,
		Updated: response.Content.Updated,
	}
	for _, space := range response.Content.ExpandedInfo.Spaces {
		detail.Spaces = append(detail.Spaces, ExpandedSpace{
			ID: space.ID, ETag: space.ETag, Name: space.Name,
			FormalName: space.FormalName, MaxCapacity: space.MaxCapacity,
		})
	}
	for _, state := range response.Content.ExpandedInfo.States {
		detail.States = append(detail.States, EventState{ID: state.ID, Name: state.Name})
	}
	for _, source := range response.Content.Data.Items {
		event := Event{
			ID: source.ID, ETag: source.ETag, Name: source.Name, Title: source.Title,
			EventLocator: source.EventLocator, Priority: source.Priority, Active: source.Active,
			Updated: source.Updated, StartDate: source.Dates.Start, EndDate: source.Dates.End,
			State: source.Context.State, TypeID: source.Context.TypeID, ParentID: source.Context.ParentID,
		}
		for _, sourceProfile := range source.Profiles {
			profile := Profile{
				ID: sourceProfile.ID, Name: sourceProfile.Name, ExpectedCount: sourceProfile.ExpectedCount,
				Registered: sourceProfile.Registered,
				Recurrence: OccurrenceDefinition{
					RecurrenceTypeID: sourceProfile.OccurrenceDefn.RecurrenceTypeID,
					ProfileCode:      sourceProfile.OccurrenceDefn.ProfileCode,
					InitialStart:     sourceProfile.OccurrenceDefn.InitialStart,
					InitialEnd:       sourceProfile.OccurrenceDefn.InitialEnd,
				},
			}
			for _, sourceReservation := range sourceProfile.Reservations {
				reservation := Reservation{
					ID: sourceReservation.ID, State: sourceReservation.State,
					Start: sourceReservation.Start, End: sourceReservation.End,
					EventStart: sourceReservation.EventStart, EventEnd: sourceReservation.EventEnd,
				}
				for _, group := range sourceReservation.Spaces {
					for _, sourceSpace := range group.Reserved {
						reservation.Spaces = append(reservation.Spaces, ReservationSpace{
							ID: sourceSpace.ID, Shared: sourceSpace.Shared,
							Attendance: sourceSpace.Attendance, Rating: sourceSpace.Rating,
						})
					}
				}
				profile.Reservations = append(profile.Reservations, reservation)
			}
			event.Profiles = append(event.Profiles, profile)
		}
		detail.Events = append(detail.Events, event)
	}
	return detail
}
