package servicearea

import (
	"github.com/claudioed/node-topology/internal/domain/node"
	"github.com/claudioed/node-topology/internal/domain/region"
	"github.com/claudioed/node-topology/internal/domain/shared"
)

// ServiceAreaAssigned is raised by Assign and Reassign. Its fields are the
// model's data list; RegionID is the station's region.
type ServiceAreaAssigned struct {
	shared.Header
	PostalPrefix          PostalPrefix
	DeliveryStationNodeID node.ID
	RegionID              region.ID
	AssignmentVersion     int
}

// EventName returns "ServiceAreaAssigned".
func (ServiceAreaAssigned) EventName() string { return "ServiceAreaAssigned" }

// Entity returns "servicearea".
func (ServiceAreaAssigned) Entity() string { return "servicearea" }

// SubjectID returns the postalPrefix.
func (e ServiceAreaAssigned) SubjectID() string { return string(e.PostalPrefix) }

var _ shared.Event = ServiceAreaAssigned{}
