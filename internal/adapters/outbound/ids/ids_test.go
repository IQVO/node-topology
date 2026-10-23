package ids_test

import (
	"regexp"
	"testing"

	"github.com/claudioed/node-topology/internal/adapters/outbound/ids"
	"github.com/claudioed/node-topology/internal/domain/shared"
)

const uuidV4 = `[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}`

func TestUUIDMintsPrefixedValidDistinctIDs(t *testing.T) {
	g := ids.UUID{}
	cases := []struct {
		kind string
		mint func() string
	}{
		{"node", func() string { return string(g.NewNodeID()) }},
		{"lane", func() string { return string(g.NewLaneID()) }},
		{"region", func() string { return string(g.NewRegionID()) }},
	}
	for _, c := range cases {
		t.Run(c.kind, func(t *testing.T) {
			first, second := c.mint(), c.mint()
			if !regexp.MustCompile(`^` + c.kind + `-` + uuidV4 + `$`).MatchString(first) {
				t.Fatalf("id = %q, want %s-<uuid v4>", first, c.kind)
			}
			if !shared.ValidID(first) {
				t.Fatalf("id %q is not a valid aggregate id", first)
			}
			if first == second {
				t.Fatalf("two ids are both %q", first)
			}
		})
	}
}
