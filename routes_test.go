package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/accessapi"
	"github.com/prusa3d/prusa-connect/sdk/routesapi"
)

// fakeAccess answers access.check from a fixed list of people who may observe
// which printer, and remembers what it was asked.
type fakeAccess struct {
	observers map[string]string // person -> printer
	fail      bool
	asked     []accessapi.CheckParams
}

func (f *fakeAccess) Check(_ context.Context, params accessapi.CheckParams) (accessapi.Decision, error) {
	f.asked = append(f.asked, params)
	if f.fail {
		return accessapi.Decision{}, errors.New("the server could not answer")
	}

	allowed := params.Action == actionObserve && params.ObjectType == objectDevice &&
		f.observers[params.Subject] == params.ObjectID

	return accessapi.Decision{Allowed: allowed}, nil
}

// recording is a recorder in the middle of a print on printer p-1.
func recording(access checker) *recorder {
	r := newRecorder()
	r.access = access
	r.status = Status{
		Configured: true, DeviceID: "p-1", DeviceName: "Core One", Storage: "local",
		Folder: "zeitraffer/core-one", Recording: true, Job: "benchy", Layer: 12,
	}
	r.preview = []byte{0xff, 0xd8, 0xff, 0xd9}

	return r
}

// serve sends one request as the host would, in person's name.
func serve(t *testing.T, r *recorder, person, path, query string) *httptest.ResponseRecorder {
	t.Helper()

	req, err := routesapi.Request{Method: http.MethodGet, Path: path, Query: query, Principal: person}.HTTP(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	w := httptest.NewRecorder()
	r.routes().ServeHTTP(w, req)

	return w
}

func routeStatus(t *testing.T, w *httptest.ResponseRecorder) Status {
	t.Helper()

	var status Status
	if err := json.Unmarshal(w.Body.Bytes(), &status); err != nil {
		t.Fatalf("read the status: %v (%s)", err, w.Body.String())
	}

	return status
}

func TestSomeoneWhoMayObserveThePrinterSeesItsRecording(t *testing.T) {
	access := &fakeAccess{observers: map[string]string{"user:alpha": "p-1"}}
	r := recording(access)

	status := routeStatus(t, serve(t, r, "user:alpha", "/status", "device=p-1"))
	if !status.ThisPrinter || !status.Recording || status.Job != "benchy" || status.DeviceName != "Core One" {
		t.Fatalf("an observer sees %+v", status)
	}

	if w := serve(t, r, "user:alpha", "/preview", ""); w.Code != http.StatusOK || w.Body.Len() == 0 {
		t.Fatalf("an observer gets the picture with %d and %d bytes", w.Code, w.Body.Len())
	}

	want := accessapi.CheckParams{Subject: "user:alpha", Action: "observe", ObjectType: "device", ObjectID: "p-1"}
	if len(access.asked) == 0 || access.asked[0] != want {
		t.Fatalf("the module asked %+v, want %+v", access.asked, want)
	}
}

func TestSomeoneWhoMayNotObserveThePrinterSeesNothingOfIt(t *testing.T) {
	// beta may observe another printer, not the one the recorder follows.
	access := &fakeAccess{observers: map[string]string{"user:beta": "p-2"}}
	r := recording(access)

	status := routeStatus(t, serve(t, r, "user:beta", "/status", "device=p-1"))
	if status.ThisPrinter || status.Recording || status.Job != "" || status.DeviceID != "" ||
		status.DeviceName != "" || status.Folder != "" || status.Layer != 0 {
		t.Fatalf("someone without observe sees %+v", status)
	}

	if !status.Configured {
		t.Fatal("the status no longer says that a camera is set up")
	}

	if w := serve(t, r, "user:beta", "/preview", ""); w.Code != http.StatusNoContent || w.Body.Len() != 0 {
		t.Fatalf("someone without observe gets the picture with %d and %d bytes", w.Code, w.Body.Len())
	}
}

func TestTheRoutesFailClosed(t *testing.T) {
	for name, tc := range map[string]struct {
		access checker
		person string
	}{
		"the server cannot answer": {access: &fakeAccess{fail: true}, person: "user:alpha"},
		"no person":                {access: &fakeAccess{observers: map[string]string{"": "p-1"}}, person: ""},
		"no access client":         {access: nil, person: "user:alpha"},
	} {
		t.Run(name, func(t *testing.T) {
			r := recording(tc.access)

			status := routeStatus(t, serve(t, r, tc.person, "/status", "device=p-1"))
			if status.Recording || status.DeviceID != "" || status.Job != "" {
				t.Fatalf("the status shows %+v", status)
			}

			if w := serve(t, r, tc.person, "/preview", ""); w.Code != http.StatusNoContent {
				t.Fatalf("the picture answers %d", w.Code)
			}
		})
	}
}

func TestBeforeAnyPrinterIsFoundThereIsNothingToHide(t *testing.T) {
	access := &fakeAccess{}
	r := newRecorder()
	r.access = access
	r.status = Status{Configured: true, Storage: "local"}

	status := routeStatus(t, serve(t, r, "user:alpha", "/status", "device=p-1"))
	if !status.Configured || !status.ThisPrinter {
		t.Fatalf("an unbound recorder answers %+v", status)
	}

	if len(access.asked) != 0 {
		t.Fatalf("the module asked about %+v without a printer", access.asked)
	}
}
