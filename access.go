package main

import (
	"context"
	"strings"

	"github.com/prusa3d/prusa-connect/sdk/accessapi"
)

// The host lets anyone who may open the extension call its routes, but the
// recorder follows one printer. Before /status or /preview shows anything of
// that printer, the module asks the server whether the person on the page may
// observe it. observe is already in permissions:, so the module may ask about
// it without a checks: entry.
const (
	actionObserve = "observe"
	objectDevice  = "device"
)

// checker is the part of the access client the routes need, so a test can
// stand in a fake.
type checker interface {
	Check(ctx context.Context, params accessapi.CheckParams) (accessapi.Decision, error)
}

// mayObserve reports whether person may observe the printer device. It fails
// closed: no person, no printer, an error or a refusal all answer false.
func mayObserve(ctx context.Context, access checker, person, device string) bool {
	if access == nil || strings.TrimSpace(person) == "" || strings.TrimSpace(device) == "" {
		// An empty subject would ask about the module itself.
		return false
	}

	decision, err := access.Check(ctx, accessapi.CheckParams{
		Subject:    person,
		Action:     actionObserve,
		ObjectType: objectDevice,
		ObjectID:   device,
	})

	return err == nil && decision.Allowed
}
