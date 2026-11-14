package servicearea

import (
	"github.com/claudioed/node-topology/internal/domain/node"
	"github.com/claudioed/node-topology/internal/domain/region"
	"github.com/claudioed/node-topology/internal/domain/shared"
)

// ServiceAreaAssigned is raised by Assign and Reassign. Its fields are the
// model's data list; RegionID is the station's region. DeliveryMode and
// Carriers are omitted on the wire when the area is a NETWORK_ROUTE area
// (ADR 0016), so a consumer that predates the mode decodes the event with
// its old meaning.
type ServiceAreaAssigned struct {
	shared.Header
	PostalPrefix          PostalPrefix
	DeliveryStationNodeID node.ID
	RegionID              region.ID
	AssignmentVersion     int
	DeliveryMode          DeliveryMode
	Carriers              []CarrierOption
}

// EventName returns "ServiceAreaAssigned".
func (ServiceAreaAssigned) EventName() string { return "ServiceAreaAssigned" }

// Entity returns "servicearea".
func (ServiceAreaAssigned) Entity() string { return "servicearea" }

// SubjectID returns the postalPrefix.
func (e ServiceAreaAssigned) SubjectID() string { return string(e.PostalPrefix) }

var _ shared.Event = ServiceAreaAssigned{}
