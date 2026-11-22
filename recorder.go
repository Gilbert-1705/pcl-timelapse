package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/prusa3d/prusa-connect/sdk/busapi"
	"github.com/prusa3d/prusa-connect/sdk/deviceapi"
	"github.com/prusa3d/prusa-connect/sdk/event"
	"github.com/prusa3d/prusa-connect/sdk/storageapi"
	"github.com/prusa3d/prusa-connect/sdk/wirerpc"
)

// recoveryGrace is how long a recording left over from before a restart
// waits for the end of its print to arrive on the resumed event stream before
// it is finished with the outcome "unknown".
const recoveryGrace = 45 * time.Second

// The watchdog compares a running recording with what the printer says. A
// print whose end never arrived (a missed event, a start read again after a
// restart) would otherwise keep the camera open for ever.
var (
	watchdogEvery = 15 * time.Second
	watchdogIdle  = 90 * time.Second
)

type jobEvent struct {
	settled  bool
	deviceID string
	job      event.JobSettled // JobStarted carries a subset of the same fields
}

type zReading struct {
	deviceID string
	at       time.Time
	z        float64
}

// Status is what the tab shows.
type Status struct {
	Configured  bool      `json:"configured"`
	DeviceID    string    `json:"device_id,omitempty"`
	DeviceName  string    `json:"device_name,omitempty"`
	ThisPrinter bool      `json:"this_printer"`
	Storage     string    `json:"storage"`
	Folder      string    `json:"folder,omitempty"`
	Recording   bool      `json:"recording"`
	Job         string    `json:"job,omitempty"`
	Started     time.Time `json:"started,omitzero"`
	Layer       int       `json:"layer,omitempty"`
	Z           float64   `json:"z,omitempty"`
	Kept        int       `json:"kept,omitempty"`
	InLayer     int       `json:"frames_in_layer,omitempty"`
	LastFrameAt time.Time `json:"last_frame_at,omitzero"`
	Rendering   int       `json:"rendering,omitempty"`
	LastVideo   string    `json:"last_video,omitempty"`
	LastError   string    `json:"last_error,omitempty"`
	// FFmpeg is the first line of `ffmpeg -version`, FFmpegProblem says in
	// plain words why ffmpeg cannot run. Nothing is recorded without it.
	FFmpeg        string `json:"ffmpeg,omitempty"`
	FFmpegProblem string `json:"ffmpeg_problem,omitempty"`
}

type recorder struct {
	once sync.Once
	wg   sync.WaitGroup

	conn     *wirerpc.Conn
	settings Settings
	dataDir  string

	jobs   chan jobEvent
	zs     chan zReading
	frames chan frame

	// Owned by the loop goroutine.
	active    *session
	stopFeed  context.CancelFunc
	leftovers map[string]leftover
	lastCheck time.Time
	idleSince time.Time

	mu      sync.Mutex
	status  Status
	preview []byte
}

type leftover struct {
	s        *session
	deadline time.Time
}

func newRecorder() *recorder {
	return &recorder{
		jobs:      make(chan jobEvent, 16),
		zs:        make(chan zReading, 64),
		frames:    make(chan frame, 8),
		leftovers: map[string]leftover{},
	}
}

func (r *recorder) start(ctx context.Context, conn *wirerpc.Conn, settings Settings, dataDir string) {
	r.once.Do(func() {
		r.conn, r.settings, r.dataDir = conn, settings, dataDir
		r.setStatus(func(s *Status) {
			s.Configured = settings.configured()
			s.Storage = settings.Storage
		})

		if !settings.configured() {
			fmt.Fprintf(os.Stderr, "%s: no camera_url is set, so nothing is recorded\n", name)

			return
		}

		r.wg.Add(4)

		go func() { defer r.wg.Done(); r.checkFFmpeg(ctx) }()
		go func() { defer r.wg.Done(); r.followJobs(ctx) }()
		go func() { defer r.wg.Done(); r.followTelemetry(ctx) }()
		go func() { defer r.wg.Done(); r.loop(ctx) }()
	})
}

func (r *recorder) wait() { r.wg.Wait() }

func (r *recorder) setStatus(change func(*Status)) {
	r.mu.Lock()
	defer r.mu.Unlock()
	change(&r.status)
}

// checkFFmpeg asks whether ffmpeg runs and shows the answer in the status, so
// a missing ffmpeg shows on the tab before the first print and not as a failed
// recording afterwards. It answers whether ffmpeg is usable.
func (r *recorder) checkFFmpeg(ctx context.Context) bool {
	version, err := checkFFmpeg(ctx, r.settings.FFmpeg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", name, err)
	}

	r.setStatus(func(s *Status) {
		s.FFmpeg = version
		s.FFmpegProblem = ""

		if err != nil {
			s.FFmpegProblem = err.Error()
		}
	})

	return err == nil
}

func (r *recorder) fail(format string, args ...any) {
	message := fmt.Sprintf(format, args...)
	fmt.Fprintf(os.Stderr, "%s: %s\n", name, message)
	r.setStatus(func(s *Status) { s.LastError = time.Now().Format("02.01. 15:04") + " " + message })
}

// --- the event stream -------------------------------------------------------

// followJobs reads the starts and ends of prints. Both are state events, so
// the stream is resumed from the saved cursor after a restart and no end of a
// print is lost.
func (r *recorder) followJobs(ctx context.Context) {
	cursorFile := filepath.Join(r.dataDir, "cursor")

	for ctx.Err() == nil {
		saved, _ := os.ReadFile(cursorFile)
		params := busapi.SubscribeParams{
			Types:  []string{event.TypeDeviceJobStarted, event.TypeDeviceJobSettled},
			Resume: strings.TrimSpace(string(saved)),
		}

		err := busapi.Subscribe(ctx, r.conn, params, func(window busapi.Window) error {
			for _, raw := range window.Events {
				var envelope event.Envelope
				if json.Unmarshal(raw, &envelope) != nil {
					continue
				}

				// The window can carry more than the two types asked for, such as
				// the marker of a gap in the stream. Only a start or an end of a
				// print with a job id is one.
				if envelope.Type != event.TypeDeviceJobStarted && envelope.Type != event.TypeDeviceJobSettled {
					continue
				}

				var job event.JobSettled
				if json.Unmarshal(envelope.Data, &job) != nil || job.JobID == "" {
					continue
				}

				select {
				case r.jobs <- jobEvent{
					settled:  envelope.Type == event.TypeDeviceJobSettled,
					deviceID: deviceOf(envelope.Subject),
					job:      job,
				}:
				case <-ctx.Done():
					return ctx.Err()
				}
			}

			if window.Cursor != "" {
				_ = os.WriteFile(cursorFile, []byte(window.Cursor), 0o640)
			}

			return nil
		})
		if err != nil && ctx.Err() == nil {
			// A cursor the host no longer holds is read again from now on.
			if strings.Contains(err.Error(), "cursor") || strings.Contains(err.Error(), "resume") {
				_ = os.Remove(cursorFile)
			}

			fmt.Fprintf(os.Stderr, "%s: job events: %v\n", name, err)
		}

		sleep(ctx, 2*time.Second)
	}
}

// followTelemetry reads the Z height of every printer it may see.
func (r *recorder) followTelemetry(ctx context.Context) {
	for ctx.Err() == nil {
		params := busapi.SubscribeParams{
			Types:      []string{event.TypeDeviceTelemetrySampled},
			IntervalMS: 500,
		}

		err := busapi.Subscribe(ctx, r.conn, params, func(window busapi.Window) error {
			for _, raw := range window.Telemetry {
				reading, ok := zOf(raw)
				if !ok {
					continue
				}

				select {
				case r.zs <- reading:
				case <-ctx.Done():
					return ctx.Err()
				}
			}

			return nil
		})
		if err != nil && ctx.Err() == nil {
			fmt.Fprintf(os.Stderr, "%s: telemetry: %v\n", name, err)
		}

		sleep(ctx, 2*time.Second)
	}
}

// zOf reads a Z height out of one telemetry envelope.
func zOf(raw json.RawMessage) (zReading, bool) {
	var envelope event.Envelope
	if json.Unmarshal(raw, &envelope) != nil {
		return zReading{}, false
	}

	var sample struct {
		Path  string  `json:"path"`
		Value float64 `json:"value"`
	}

	if json.Unmarshal(envelope.Data, &sample) != nil || sample.Path != "/axis/z" {
		return zReading{}, false
	}

	at := envelope.Time
	if at.IsZero() {
		at = time.Now()
	}

	return zReading{deviceID: deviceOf(envelope.Subject), at: at, z: sample.Value}, true
}

func deviceOf(subject string) string {
	kind, id, found := strings.Cut(subject, ":")
	if !found || kind != string(event.KindDevice) {
		return ""
	}

	return id
}

// --- the loop ---------------------------------------------------------------

func (r *recorder) loop(ctx context.Context) {
	r.bind(ctx)
	r.recover(ctx)

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	defer func() {
		if r.stopFeed != nil {
			r.stopFeed()
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return
		case job := <-r.jobs:
			r.onJob(ctx, job)
		case reading := <-r.zs:
			if r.active != nil && reading.deviceID == r.active.meta.DeviceID {
				r.active.tracker.observe(reading.at, reading.z)
				r.setStatus(func(s *Status) { s.Z = reading.z })
			}
		case f := <-r.frames:
			r.onFrame(f)
		case now := <-ticker.C:
			r.onTick(ctx, now)
		}
	}
}

// bind decides which printer the camera watches: the one the printer setting
// names, or the only printer the server has.
func (r *recorder) bind(ctx context.Context) {
	page, err := deviceapi.NewClient(r.conn).List(ctx, deviceapi.ListParams{Limit: 200})
	if err != nil {
		r.fail("the printers could not be read: %v", err)

		return
	}

	want := strings.TrimSpace(r.settings.Printer)

	var found []deviceapi.Device

	for _, d := range page.Items {
		if d.Retired {
			continue
		}

		if want == "" || d.ID == want || strings.EqualFold(d.Name, want) {
			found = append(found, d)
		}
	}

	switch {
	case len(found) == 1:
		r.setStatus(func(s *Status) {
			s.DeviceID, s.DeviceName = found[0].ID, found[0].Name
			s.Folder = printerFolder(r.settings, found[0].Name, found[0].ID)
		})
	case want != "":
		r.fail("no printer is called %q; the first printer that starts a print is taken", want)
	case len(found) > 1:
		fmt.Fprintf(os.Stderr, "%s: %d printers and no printer setting: the first one that starts a print is taken\n",
			name, len(found))
	}
}

func (r *recorder) boundDevice() (string, string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	return r.status.DeviceID, r.status.DeviceName
}

func (r *recorder) onJob(ctx context.Context, job jobEvent) {
	if job.settled {
		if r.active != nil && r.active.meta.JobID == job.job.JobID {
			r.finish(ctx, r.active, job.job.Outcome)
			r.active = nil
			r.stopFeedNow()

			return
		}

		if left, ok := r.leftovers[job.job.JobID]; ok {
			delete(r.leftovers, job.job.JobID)
			r.finish(ctx, left.s, job.job.Outcome)
		}

		return
	}

	id, deviceName := r.boundDevice()

	switch {
	case id == "":
		// The first printer that starts a print is the one the camera watches.
		d, err := deviceapi.NewClient(r.conn).Get(ctx, deviceapi.GetParams{ID: job.deviceID})
		if err == nil {
			deviceName = d.Name
		}

		id = job.deviceID
		r.setStatus(func(s *Status) {
			s.DeviceID, s.DeviceName = id, deviceName
			s.Folder = printerFolder(r.settings, deviceName, id)
		})
	case id != job.deviceID:
		return
	}

	if r.active != nil {
		if r.active.meta.JobID == job.job.JobID {
			return // the same start, read again after a reconnect
		}

		// A new print while one is still recorded: its end was missed.
		r.finish(ctx, r.active, "unknown")
		r.active = nil
		r.stopFeedNow()
	}

	m := meta{
		JobID:       job.job.JobID,
		DeviceJobID: job.job.DeviceJobID,
		DeviceID:    id,
		DeviceName:  deviceName,
		DisplayName: displayName(job.job),
		Started:     time.Now(),
	}

	if !job.job.Started.IsZero() {
		m.Started = job.job.Started
	}

	s, err := openSession(filepath.Join(r.dataDir, "sessions", m.JobID), m, r.settings.settle, r.settings.MaxPerLayer)
	if s != nil {
		s.setDiagnosis(r.settings.DiagnoseLayers, r.settings.DiagnoseEvery)
		s.setPark(r.settings.GcodePark, r.settings.parkWindow)
		s.fillName(ctx, r.conn)
	}
	if err != nil {
		r.fail("the recording could not be set up: %v", err)

		return
	}

	r.begin(ctx, s)
}

func displayName(job event.JobSettled) string {
	if job.DisplayName != "" {
		return job.DisplayName
	}

	if job.Path != "" {
		return filepath.Base(job.Path)
	}

	return placeholderName
}

// placeholderName stands in for a print whose file name the start did not
// carry. The name is asked for again while recording and before storing.
const placeholderName = "druck"

// jobName asks the record store for the file name of the print. The start of
// a print can arrive before the printer has reported which file it prints.
func jobName(ctx context.Context, conn *wirerpc.Conn, m meta) string {
	jobs, err := deviceapi.NewClient(conn).Jobs(ctx, deviceapi.JobsParams{ID: m.DeviceID})
	if err != nil {
		return ""
	}

	for _, job := range []*deviceapi.Job{jobs.Current, jobs.Last} {
		if job == nil || job.ID != m.JobID {
			continue
		}

		if job.DisplayName != "" {
			return job.DisplayName
		}

		if job.Path != "" {
			return filepath.Base(job.Path)
		}
	}

	return ""
}

// fillName takes the print's file name from the record store once it knows
// the job. The store is asked even when the start carried a name: the start
// of a print can carry the name of the print before it, so the store is asked
// at the start, on every watchdog tick and before storing. It answers whether
// the name changed.
func (s *session) fillName(ctx context.Context, conn *wirerpc.Conn) bool {
	found := jobName(ctx, conn, s.meta)
	if found == "" {
		return false
	}

	if found == s.meta.DisplayName {
		return false
	}

	s.meta.DisplayName = found
	_ = s.save()

	return true
}

// begin starts recording a session: the tracker learns the current height and
// the camera feed opens.
func (r *recorder) begin(ctx context.Context, s *session) {
	z, known := currentZ(ctx, r.conn, s.meta.DeviceID)
	s.tracker.reset(time.Now(), z, known)
	r.active = s
	r.idleSince, r.lastCheck = time.Time{}, time.Now()

	// Without ffmpeg there is nothing to decode the stream or build the video
	// with. The print is not recorded, and the tab says why.
	if !r.checkFFmpeg(ctx) {
		r.fail("not recorded: ffmpeg is missing, see the notice above")

		return
	}

	source, err := sourceFor(r.settings)
	if err != nil {
		r.fail("%v", err)

		return
	}

	feedCtx, cancel := context.WithCancel(ctx)
	r.stopFeed = cancel

	r.wg.Add(1)

	go func() {
		defer r.wg.Done()
		r.feed(feedCtx, source)
	}()

	r.setStatus(func(st *Status) {
		st.Recording, st.Job, st.Started = true, s.meta.DisplayName, s.meta.Started
		st.Layer, st.Kept, st.InLayer = s.meta.Layers+1, s.meta.Picked, 0
		st.Z = z
	})
	r.setPreview(s.preview())

	fmt.Fprintf(os.Stderr, "%s: recording %q on %s\n", name, s.meta.DisplayName, s.meta.DeviceID)
}

// feed keeps the camera open for as long as the print runs, and opens it
// again when it drops.
func (r *recorder) feed(ctx context.Context, source frameSource) {
	backoff := 2 * time.Second

	for ctx.Err() == nil {
		started := time.Now()
		err := source.run(ctx, r.frames)

		if ctx.Err() != nil {
			return
		}

		if err != nil {
			r.fail("camera: %v", err)
		}

		if time.Since(started) > time.Minute {
			backoff = 2 * time.Second
		}

		sleep(ctx, backoff)
		backoff = min(backoff*2, time.Minute)
	}
}

func (r *recorder) stopFeedNow() {
	if r.stopFeed != nil {
		r.stopFeed()
		r.stopFeed = nil
	}

	// Frames already read belong to nobody now.
	for {
		select {
		case <-r.frames:
		default:
			r.setStatus(func(s *Status) { s.Recording, s.InLayer = false, 0 })

			return
		}
	}
}

func (r *recorder) onFrame(f frame) {
	if r.active == nil {
		return
	}

	if err := r.active.add(f); err != nil {
		r.fail("%v", err)

		return
	}

	inLayer := len(r.active.layer)
	r.setStatus(func(s *Status) { s.InLayer, s.LastFrameAt = inLayer, f.at })
}

func (r *recorder) onTick(ctx context.Context, now time.Time) {
	if s := r.active; s != nil {
		if boundary, ok := s.tracker.tick(now); ok {
			kept, err := s.closeLayer(boundary)
			if err != nil {
				r.fail("a layer could not be kept: %v", err)
			}

			if kept {
				r.setPreview(s.preview())
			}

			inLayer := len(s.layer)
			r.setStatus(func(st *Status) {
				st.Layer, st.Kept, st.InLayer = s.meta.Layers+1, s.meta.Picked, inLayer
			})
		}
	}

	if r.active != nil && now.Sub(r.lastCheck) >= watchdogEvery {
		r.lastCheck = now
		r.watchdog(ctx, now)
	}

	for jobID, left := range r.leftovers {
		if now.After(left.deadline) {
			delete(r.leftovers, jobID)
			r.finish(ctx, left.s, "unknown")
		}
	}
}

// watchdog ends a recording once the printer has stopped printing for
// watchdogIdle, with the outcome its state suggests.
func (r *recorder) watchdog(ctx context.Context, now time.Time) {
	if r.active.fillName(ctx, r.conn) {
		r.setStatus(func(st *Status) { st.Job = r.active.meta.DisplayName })
	}

	state, ok := printerState(ctx, r.conn, r.active.meta.DeviceID)
	if !ok || activeState(state) {
		r.idleSince = time.Time{}

		return
	}

	if r.idleSince.IsZero() {
		r.idleSince = now

		return
	}

	if now.Sub(r.idleSince) < watchdogIdle {
		return
	}

	outcome := "unknown"

	switch strings.ToUpper(state) {
	case "FINISHED":
		outcome = "finished"
	case "STOPPED":
		outcome = "cancelled"
	case "ERROR":
		outcome = "failed"
	}

	fmt.Fprintf(os.Stderr, "%s: the printer reports %s and no end of %q arrived; the recording ends\n",
		name, state, r.active.meta.DisplayName)
	r.finish(ctx, r.active, outcome)
	r.active = nil
	r.idleSince = time.Time{}
	r.stopFeedNow()
}

// activeState reports whether a printer in this state may be in a print:
// printing, paused, busy between moves or waiting for the operator mid-print.
func activeState(state string) bool {
	switch strings.ToUpper(state) {
	case "PRINTING", "PAUSED", "BUSY", "ATTENTION":
		return true
	}

	return false
}

func printerState(ctx context.Context, conn *wirerpc.Conn, deviceID string) (string, bool) {
	d, err := deviceapi.NewClient(conn).Get(ctx, deviceapi.GetParams{ID: deviceID})
	if err != nil {
		return "", false
	}

	var state string
	if json.Unmarshal(d.State["/state"], &state) != nil || state == "" {
		return "", false
	}

	return state, true
}

func (r *recorder) setPreview(p []byte) {
	if len(p) == 0 {
		return
	}

	r.mu.Lock()
	r.preview = p
	r.mu.Unlock()
}

// recover picks up recordings a restart interrupted. One whose print is still
// running carries on; the others wait for their end on the resumed stream.
func (r *recorder) recover(ctx context.Context) {
	dirs, _ := filepath.Glob(filepath.Join(r.dataDir, "sessions", "*"))

	for _, dir := range dirs {
		m, err := loadMeta(dir)
		if err != nil {
			_ = os.RemoveAll(dir)

			continue
		}

		s, err := openSession(dir, m, r.settings.settle, r.settings.MaxPerLayer)
		if err != nil {
			continue
		}

		s.setDiagnosis(r.settings.DiagnoseLayers, r.settings.DiagnoseEvery)
		s.setPark(r.settings.GcodePark, r.settings.parkWindow)

		if r.active == nil && stillPrinting(ctx, r.conn, m) {
			fmt.Fprintf(os.Stderr, "%s: carrying on with %q after a restart\n", name, m.DisplayName)
			r.begin(ctx, s)

			continue
		}

		r.leftovers[m.JobID] = leftover{s: s, deadline: time.Now().Add(recoveryGrace)}
	}
}

func currentZ(ctx context.Context, conn *wirerpc.Conn, deviceID string) (float64, bool) {
	d, err := deviceapi.NewClient(conn).Get(ctx, deviceapi.GetParams{ID: deviceID})
	if err != nil {
		return 0, false
	}

	var z float64
	if raw, ok := d.State["/axis/z"]; ok && json.Unmarshal(raw, &z) == nil {
		return z, true
	}

	return 0, false
}

func stillPrinting(ctx context.Context, conn *wirerpc.Conn, m meta) bool {
	d, err := deviceapi.NewClient(conn).Get(ctx, deviceapi.GetParams{ID: m.DeviceID})
	if err != nil {
		return false
	}

	var state string
	_ = json.Unmarshal(d.State["/state"], &state)

	var jobID json.Number
	_ = json.Unmarshal(d.State["/job/id"], &jobID)

	printing := strings.EqualFold(state, "PRINTING") || strings.EqualFold(state, "PAUSED")

	return printing && (m.DeviceJobID == "" || jobID.String() == m.DeviceJobID)
}

// --- the end of a print -----------------------------------------------------

// finish closes the last layer and renders and stores the video in the
// background, so the next print starts recording at once.
func (r *recorder) finish(ctx context.Context, s *session, outcome string) {
	if _, err := s.closeLayer(time.Time{}); err != nil {
		r.fail("the last layer could not be kept: %v", err)
	}

	r.setStatus(func(st *Status) { st.Rendering++ })
	r.wg.Add(1)

	go func() {
		defer r.wg.Done()
		defer r.setStatus(func(st *Status) { st.Rendering-- })

		folder, err := r.store(ctx, s, outcome)
		if err != nil {
			r.fail("the timelapse of %q was not stored: %v", s.meta.DisplayName, err)

			return
		}

		if folder != "" {
			fmt.Fprintf(os.Stderr, "%s: stored %s/%s\n", name, r.settings.Storage, folder)
			r.setStatus(func(st *Status) { st.LastVideo = folder })
		}

		_ = os.RemoveAll(s.dir)
	}()
}

// store renders the video and writes it, the cover and a description into
// the storage. A print with fewer layers than min_layers is dropped.
func (r *recorder) store(ctx context.Context, s *session, outcome string) (string, error) {
	if s.meta.Picked < r.settings.MinLayers {
		fmt.Fprintf(os.Stderr, "%s: %q has %d layers, fewer than min_layers %d, so no timelapse is kept\n",
			name, s.meta.DisplayName, s.meta.Picked, r.settings.MinLayers)

		return "", nil
	}

	s.fillName(ctx, r.conn)

	video, err := s.render(ctx, r.settings)
	if err != nil {
		return "", err
	}

	folder := runFolder(r.settings, s.meta)
	files := storageapi.NewClient(r.conn)

	if err = writeFile(ctx, files, r.settings.Storage, folder+"/zeitraffer.mp4", video); err != nil {
		return "", err
	}

	if err = writeFile(ctx, files, r.settings.Storage, folder+"/vorschau.jpg", s.pickedPath(s.meta.Picked)); err != nil {
		return "", err
	}

	var videoBytes int64
	if st, err := os.Stat(video); err == nil {
		videoBytes = st.Size()
	}

	info, _ := json.MarshalIndent(map[string]any{
		"druck":       s.meta.DisplayName,
		"drucker":     s.meta.DeviceName,
		"drucker_id":  s.meta.DeviceID,
		"job_id":      s.meta.JobID,
		"gestartet":   s.meta.Started,
		"beendet":     time.Now(),
		"ergebnis":    outcome,
		"schichten":   s.meta.Layers,
		"bilder":      s.meta.Picked,
		"video_fps":   r.settings.VideoFPS,
		"video_bytes": videoBytes,
		"diagnose":    s.meta.Diagnosed,
	}, "", "  ")

	if err = writeBytes(ctx, files, r.settings.Storage, folder+"/info.json", info); err != nil {
		return "", err
	}

	if s.meta.Diagnosed > 0 {
		if err = storeDiagnosis(ctx, files, r.settings.Storage, folder, s.dir); err != nil {
			// The video is stored; a diagnosis that did not make it is not
			// worth failing the run over.
			r.fail("the diagnosis of %q was not stored: %v", s.meta.DisplayName, err)
		}
	}

	if r.settings.KeepFrames {
		for n := 1; n <= s.meta.Picked; n++ {
			target := fmt.Sprintf("%s/bilder/%06d.jpg", folder, n)
			if err = writeFile(ctx, files, r.settings.Storage, target, s.pickedPath(n)); err != nil {
				return "", err
			}
		}
	}

	return folder, nil
}

func writeFile(ctx context.Context, files *storageapi.Client, storage, target, source string) error {
	f, err := os.Open(source)
	if err != nil {
		return err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return err
	}

	_, err = files.Write(ctx, storageapi.WriteParams{Storage: storage, Path: target, Size: info.Size()}, f)
	if err != nil {
		return fmt.Errorf("write %s:%s: %w", storage, target, err)
	}

	return nil
}

func writeBytes(ctx context.Context, files *storageapi.Client, storage, target string, body []byte) error {
	if len(body) == 0 {
		return errors.New("nothing to write")
	}

	_, err := files.Write(ctx, storageapi.WriteParams{Storage: storage, Path: target, Size: int64(len(body))},
		bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("write %s:%s: %w", storage, target, err)
	}

	return nil
}

func sleep(ctx context.Context, d time.Duration) {
	select {
	case <-ctx.Done():
	case <-time.After(d):
	}
}
