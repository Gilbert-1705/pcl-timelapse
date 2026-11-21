package main

import (
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/routesapi"
	"github.com/prusa3d/prusa-connect/sdk/wirerpc"
)

// The host refuses a module whose OpenAPI fragment it cannot read.
func TestTheRoutesAreDescribed(t *testing.T) {
	if _, err := routesapi.Describe(wirerpc.Info{Name: name, Version: release, Contract: wirerpc.Contract}, openAPI); err != nil {
		t.Fatal(err)
	}
}
