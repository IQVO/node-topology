package region

import "github.com/claudioed/node-topology/internal/domain/shared"

// RegionDefined is raised by Define. Its fields are the model's data list.
type RegionDefined struct {
	shared.Header
	RegionID ID
	Code     string
	Name     string
}

// EventName returns "RegionDefined".
func (RegionDefined) EventName() string { return "RegionDefined" }

// Entity returns "region".
func (RegionDefined) Entity() string { return "region" }

// SubjectID returns the regionId.
func (e RegionDefined) SubjectID() string { return string(e.RegionID) }

var _ shared.Event = RegionDefined{}
