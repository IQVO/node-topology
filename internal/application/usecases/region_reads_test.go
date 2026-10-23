package usecases_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/claudioed/node-topology/internal/application/ports"
	"github.com/claudioed/node-topology/internal/application/usecases"
)

func TestGetRegionReturnsTheRegion(t *testing.T) {
	e := newEnv(t)
	e.seedRegion("r-1", "SP")

	got, err := usecases.NewGetRegion(e.regions).Execute(context.Background(), "r-1")
	wantErr(t, err, nil)

	if got.RegionID != "r-1" || got.Code != "SP" || got.Name != "Region SP" {
		t.Fatalf("view = %+v", got)
	}
}

func TestGetRegionFailures(t *testing.T) {
	e := newEnv(t)
	_, err := usecases.NewGetRegion(e.regions).Execute(context.Background(), "ghost")
	wantErr(t, err, ports.ErrNotFound)

	e.regions.getErr = errBoom
	_, err = usecases.NewGetRegion(e.regions).Execute(context.Background(), "r-1")
	wantErr(t, err, errBoom)
}

func TestListRegionsOrdersByID(t *testing.T) {
	e := newEnv(t)
	e.seedRegion("r-b", "RJ")
	e.seedRegion("r-c", "MG")
	e.seedRegion("r-a", "SP")

	got, err := usecases.NewListRegions(e.regions).Execute(context.Background())
	wantErr(t, err, nil)

	ids, codes := []string{}, []string{}
	for _, v := range got {
		ids, codes = append(ids, v.RegionID), append(codes, v.Code)
	}
	if want := []string{"r-a", "r-b", "r-c"}; !reflect.DeepEqual(ids, want) {
		t.Fatalf("ids = %v, want %v", ids, want)
	}
	if want := []string{"SP", "RJ", "MG"}; !reflect.DeepEqual(codes, want) {
		t.Fatalf("codes = %v, want %v", codes, want)
	}
}

func TestListRegionsIsEmptyNotNil(t *testing.T) {
	e := newEnv(t)
	got, err := usecases.NewListRegions(e.regions).Execute(context.Background())
	wantErr(t, err, nil)
	if got == nil || len(got) != 0 {
		t.Fatalf("list = %#v, want a non-nil empty list", got)
	}
}

func TestListRegionsFailure(t *testing.T) {
	e := newEnv(t)
	e.regions.listErr = errBoom
	_, err := usecases.NewListRegions(e.regions).Execute(context.Background())
	wantErr(t, err, errBoom)
}
