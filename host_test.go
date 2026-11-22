package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/prusa3d/prusa-connect/sdk/busapi"
	"github.com/prusa3d/prusa-connect/sdk/deviceapi"
	"github.com/prusa3d/prusa-connect/sdk/event"
	"github.com/prusa3d/prusa-connect/sdk/storageapi"
	"github.com/prusa3d/prusa-connect/sdk/wirerpc"
)

// The fakes below stand in for Prusa Connect Local: the module talks to them
// over a real wirerpc session, through the SDK's own host-side handlers.

type fakeFeed struct {
	jobs, telemetry chan busapi.Window
}

func (f *fakeFeed) Subscribe(ctx context.Context, params busapi.SubscribeParams, write func(busapi.Window) error) error {
	source := f.telemetry
	if len(params.Types) > 0 && params.Types[0] == event.TypeDeviceJobStarted {
		source = f.jobs
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		case w := <-source:
			if err := write(w); err != nil {
				return err
			}
		}
	}
}

var errNotHere = errors.New("not in this fake")

type fakeFleet struct {
	z     atomic.Value
	state atomic.Value
}

func (f *fakeFleet) device() deviceapi.Device {
	z, _ := json.Marshal(f.z.Load())

	state := "PRINTING"
	if s, ok := f.state.Load().(string); ok {
		state = s
	}

	st, _ := json.Marshal(state)

	return deviceapi.Device{
		Kind: "device", ID: "P1", Name: "Core One", Connected: true,
		State: map[string]json.RawMessage{"/axis/z": z, "/state": st},
	}
}

func (f *fakeFleet) List(context.Context, deviceapi.ListParams) (deviceapi.Page, error) {
	return deviceapi.Page{Items: []deviceapi.Device{f.device()}, Total: 1, Limit: 200}, nil
}

func (f *fakeFleet) Get(_ context.Context, p deviceapi.GetParams) (deviceapi.Device, error) {
	if p.ID != "P1" {
		return deviceapi.Device{}, errNotHere
	}

	return f.device(), nil
}

func (f *fakeFleet) Command(context.Context, deviceapi.CommandParams) (deviceapi.Result, error) {
	return deviceapi.Result{}, errNotHere
}

func (f *fakeFleet) Approve(context.Context, deviceapi.ApproveParams) (deviceapi.Registration, error) {
	return deviceapi.Registration{}, errNotHere
}

// Jobs knows the file of print J1. Its start event carries the name of the
// print before it, as a printer that has not yet reported the new file does.
func (f *fakeFleet) Jobs(_ context.Context, p deviceapi.JobsParams) (deviceapi.Jobs, error) {
	if p.ID != "P1" {
		return deviceapi.Jobs{}, errNotHere
	}

	return deviceapi.Jobs{Current: &deviceapi.Job{
		ID: "J1", DeviceJobID: "7", Path: "/usb/WUERFE~1.BGC", DisplayName: "Würfel.bgcode",
	}}, nil
}

func (f *fakeFleet) Files(context.Context, deviceapi.FilesParams) (deviceapi.Listing, error) {
	return deviceapi.Listing{}, errNotHere
}

func (f *fakeFleet) Print(context.Context, deviceapi.PrintParams) (deviceapi.Started, error) {
	return deviceapi.Started{}, errNotHere
}

func (f *fakeFleet) JobHistory(context.Context, deviceapi.JobHistoryParams) (deviceapi.JobPage, error) {
	return deviceapi.JobPage{}, errNotHere
}

type fakeFiles struct {
	mu    sync.Mutex
	files map[string][]byte
}

func (f *fakeFiles) Stat(context.Context, storageapi.StatParams) (storageapi.FileInfo, error) {
	return storageapi.FileInfo{}, storageapi.ErrNoFile
}

func (f *fakeFiles) Read(context.Context, storageapi.ReadParams) (storageapi.Chunk, error) {
	return storageapi.Chunk{}, storageapi.ErrNoFile
}

func (f *fakeFiles) List(context.Context, storageapi.ListParams) (storageapi.Listing, error) {
	return storageapi.Listing{}, nil
}

func (f *fakeFiles) Fetch(context.Context, storageapi.FetchParams, io.Writer) (storageapi.Fetched, error) {
	return storageapi.Fetched{}, storageapi.ErrNoFile
}

func (f *fakeFiles) Write(_ context.Context, p storageapi.WriteParams, body io.Reader) (storageapi.Written, error) {
	raw, err := io.ReadAll(body)
	if err != nil {
		return storageapi.Written{}, err
	}

	f.mu.Lock()
	f.files[p.Storage+":"+p.Path] = raw
	f.mu.Unlock()

	return storageapi.Written{Path: p.Path, Size: int64(len(raw))}, nil
}

func envelope(t *testing.T, typ string, data any) json.RawMessage {
	t.Helper()

	payload, _ := json.Marshal(data)
	raw, _ := json.Marshal(event.Envelope{
		SpecVersion: event.SpecVersion, ID: typ + time.Now().String(), Source: "urn:test",
		Type: typ, Time: time.Now(), Subject: "device:P1", Class: event.ClassState, Data: payload,
	})

	return raw
}

// TestARecordedPrintEndsAsAVideoInTheStorage runs the whole module against
// the fakes: a print starts, Z rises over six layers while the camera shows
// the head moving, the print ends, and a video lands in the storage.
func TestARecordedPrintEndsAsAVideoInTheStorage(t *testing.T) {
	if testing.Short() {
		t.Skip("takes about half a minute")
	}

	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("no ffmpeg")
	}

	// The camera: a snapshot URL whose picture has the head somewhere new on
	// every request, and clear of the print on every fifth.
	var shots atomic.Int64

	camera := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := shots.Add(1)
		x := []int{250, 280, 300, 540, 270}[n%5]
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write(encode(t, scene(x)))
	}))
	defer camera.Close()

	feed := &fakeFeed{jobs: make(chan busapi.Window, 4), telemetry: make(chan busapi.Window, 64)}
	fleet := &fakeFleet{}
	fleet.z.Store(0.2)

	files := &fakeFiles{files: map[string][]byte{}}

	hostMethods := wirerpc.Methods{}
	for _, part := range []wirerpc.Methods{
		busapi.SubscribeHandler(feed), deviceapi.Handlers(fleet), storageapi.Handlers(files),
	} {
		for m, h := range part {
			hostMethods[m] = h
		}
	}

	hostSide, moduleSide := net.Pipe()

	host, err := wirerpc.NewConn(hostSide, wirerpc.Options{Role: wirerpc.RoleHost, Methods: hostMethods})
	if err != nil {
		t.Fatal(err)
	}

	module, err := wirerpc.NewConn(moduleSide, wirerpc.Options{Role: wirerpc.RoleChild, Methods: wirerpc.Methods{}})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	go func() { _ = host.Serve(ctx) }()
	go func() { _ = module.Serve(ctx) }()

	settings := Settings{
		CameraURL: camera.URL, Storage: "local", Folder: "zeitraffer",
		CaptureFPS: 5, VideoFPS: 10, LayerSettle: "2s", MinLayers: 3, MaxPerLayer: 64, FFmpeg: "ffmpeg",
		Timezone: "Europe/Berlin", DiagnoseLayers: 2, DiagnoseEvery: 2,
	}
	if err = settings.check(); err != nil {
		t.Fatal(err)
	}

	rec := newRecorder()
	rec.start(ctx, module, settings, t.TempDir())

	time.Sleep(500 * time.Millisecond)

	feed.jobs <- busapi.Window{Events: []json.RawMessage{envelope(t, event.TypeDeviceJobStarted,
		map[string]any{"job_id": "J1", "device_job_id": "7", "started": time.Now(),
			"display_name": "Vorheriger_Druck.bgcode"})}}

	z := 0.2
	for range 6 {
		time.Sleep(3 * time.Second)

		z += 0.2
		fleet.z.Store(z)
		feed.telemetry <- busapi.Window{Telemetry: []json.RawMessage{envelope(t, event.TypeDeviceTelemetrySampled,
			map[string]any{"path": "/axis/z", "value": z})}}
	}

	time.Sleep(3 * time.Second)

	status := statusOf(rec)
	if !status.Recording || status.Kept < 4 {
		t.Fatalf("while printing: %+v", status)
	}

	feed.jobs <- busapi.Window{Events: []json.RawMessage{envelope(t, event.TypeDeviceJobSettled,
		map[string]any{"job_id": "J1", "outcome": "finished"})}}

	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		files.mu.Lock()
		notes := 0
		for path := range files.files {
			if strings.HasSuffix(path, "/auswahl.json") {
				notes++
			}
		}
		files.mu.Unlock()

		// The diagnosis is written after the video; the run is done once the
		// recorder has nothing left to render.
		if st := statusOf(rec); notes >= 2 && st.Rendering == 0 && st.LastVideo != "" {
			break
		}

		time.Sleep(200 * time.Millisecond)
	}

	files.mu.Lock()
	defer files.mu.Unlock()

	var video, info []byte

	for path, body := range files.files {
		switch {
		case strings.HasSuffix(path, "/zeitraffer.mp4"):
			video = body
		case strings.HasSuffix(path, "/info.json"):
			info = body
		}

		if !strings.HasPrefix(path, "local:zeitraffer/core-one/") || !strings.Contains(path, "_wuerfel/") {
			t.Errorf("stored at %s", path)
		}
	}

	if len(video) < 1000 || !bytes.Contains(video[:64], []byte("ftyp")) {
		t.Fatalf("no MP4 was stored (%d files: %v)", len(files.files), keys(files.files))
	}

	if !bytes.Contains(info, []byte(`"ergebnis": "finished"`)) || !bytes.Contains(info, []byte(`"diagnose": 2`)) ||
		!bytes.Contains(info, []byte(`"druck": "Würfel.bgcode"`)) ||
		bytes.Contains(info, []byte(`"video_bytes": 0`)) {
		t.Fatalf("info.json: %s", info)
	}

	var notes, candidates, references int

	for path, body := range files.files {
		if !strings.Contains(path, "/diagnose/schicht-") {
			continue
		}

		switch {
		case strings.HasSuffix(path, "/auswahl.json"):
			notes++
			if !bytes.Contains(body, []byte(`"gewaehlt": true`)) {
				t.Errorf("%s names no chosen frame: %s", path, body)
			}
		case strings.HasSuffix(path, ".jpg"):
			candidates++
		case strings.HasSuffix(path, "/referenz.png"):
			references++
		}
	}

	if notes != 2 || candidates < 4 {
		t.Fatalf("diagnosis: %d notes, %d candidates, %d references (%v)", notes, candidates, references, keys(files.files))
	}

	if s := statusOf(rec); s.Recording || s.LastVideo == "" || s.LastError != "" {
		t.Fatalf("after the print: %+v", s)
	}

	cancel()
	rec.wait()
}

func statusOf(r *recorder) Status {
	r.mu.Lock()
	defer r.mu.Unlock()

	return r.status
}

func keys(m map[string][]byte) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}

	return out
}

// A start whose end never arrives (here: read again after a restart while the
// printer is idle) must not keep the camera open: the watchdog ends it.
func TestARecordingEndsWhenThePrinterIsIdleAndNoEndArrives(t *testing.T) {
	if testing.Short() {
		t.Skip("takes a few seconds")
	}

	every, idle := watchdogEvery, watchdogIdle
	watchdogEvery, watchdogIdle = 200*time.Millisecond, time.Second

	defer func() { watchdogEvery, watchdogIdle = every, idle }()

	camera := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(encode(t, scene(540)))
	}))
	defer camera.Close()

	feed := &fakeFeed{jobs: make(chan busapi.Window, 4), telemetry: make(chan busapi.Window, 4)}
	fleet := &fakeFleet{}
	fleet.z.Store(0.2)
	fleet.state.Store("IDLE")

	hostMethods := wirerpc.Methods{}
	for _, part := range []wirerpc.Methods{
		busapi.SubscribeHandler(feed), deviceapi.Handlers(fleet), storageapi.Handlers(&fakeFiles{files: map[string][]byte{}}),
	} {
		for m, h := range part {
			hostMethods[m] = h
		}
	}

	hostSide, moduleSide := net.Pipe()
	host, _ := wirerpc.NewConn(hostSide, wirerpc.Options{Role: wirerpc.RoleHost, Methods: hostMethods})
	module, _ := wirerpc.NewConn(moduleSide, wirerpc.Options{Role: wirerpc.RoleChild, Methods: wirerpc.Methods{}})

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	go func() { _ = host.Serve(ctx) }()
	go func() { _ = module.Serve(ctx) }()

	settings := Settings{CameraURL: camera.URL, Storage: "local", Folder: "zeitraffer", CaptureFPS: 5, VideoFPS: 10,
		LayerSettle: "2s", MinLayers: 3, MaxPerLayer: 64, FFmpeg: "ffmpeg"}
	_ = settings.check()

	rec := newRecorder()
	rec.start(ctx, module, settings, t.TempDir())

	time.Sleep(300 * time.Millisecond)

	// Something else on the job stream, and a start without a job id: neither
	// is a print.
	feed.jobs <- busapi.Window{Events: []json.RawMessage{
		envelope(t, event.TypeStreamContinuityLost, map[string]any{}),
		envelope(t, event.TypeDeviceJobStarted, map[string]any{}),
	}}

	time.Sleep(time.Second)

	if statusOf(rec).Recording {
		t.Fatal("an event that is no start of a print began a recording")
	}

	feed.jobs <- busapi.Window{Events: []json.RawMessage{envelope(t, event.TypeDeviceJobStarted,
		map[string]any{"job_id": "OLD"})}}

	time.Sleep(time.Second)

	if !statusOf(rec).Recording {
		t.Fatal("the start was not taken")
	}

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) && statusOf(rec).Recording {
		time.Sleep(100 * time.Millisecond)
	}

	if statusOf(rec).Recording {
		t.Fatal("still recording with the printer idle and no end of the print")
	}

	cancel()
	rec.wait()
}
