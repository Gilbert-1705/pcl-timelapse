package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// meta is what a session keeps on disk beside its frames, so a module that
// was restarted mid-print carries on with the same recording.
type meta struct {
	JobID       string    `json:"job_id"`
	DeviceJobID string    `json:"device_job_id"`
	DeviceID    string    `json:"device_id"`
	DeviceName  string    `json:"device_name"`
	DisplayName string    `json:"display_name"`
	Started     time.Time `json:"started"`
	Picked      int       `json:"picked"`
	Layers      int       `json:"layers"`
	Diagnosed   int       `json:"diagnosed,omitempty"`
}

// buffered is one frame of the layer being printed: its thumbnail in memory
// and the JPEG on disk.
type buffered struct {
	at    time.Time
	thumb []uint8
	file  string
}

// session records one print.
type session struct {
	meta    meta
	dir     string
	tracker *layerTracker
	max     int

	layer  []buffered
	stride int // keep every stride-th frame once a long layer was thinned
	seen   int // frames offered in this layer
	serial int // names the frame files

	lastPicked []byte    // the newest chosen frame, for the preview
	history    [][]uint8 // thumbnails of the last chosen frames

	diagnoseLayers int // how many layers of a print keep all their candidates
	diagnoseEvery  int // every how many layers one is kept

	park       bool          // the slicer parks the head after each layer change
	parkWindow time.Duration // how long after the start of a layer the park is looked for
}

func openSession(dir string, m meta, settle time.Duration, maxPerLayer int) (*session, error) {
	for _, sub := range []string{"layer", "picked"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o750); err != nil {
			return nil, err
		}
	}

	s := &session{meta: m, dir: dir, tracker: newLayerTracker(settle), max: maxPerLayer, stride: 1}

	// Frames of an interrupted layer are not worth sorting out: the layer is
	// started again.
	stale, _ := filepath.Glob(filepath.Join(dir, "layer", "*.jpg"))
	for _, file := range stale {
		_ = os.Remove(file)
	}

	if last := s.pickedPath(m.Picked); m.Picked > 0 {
		s.lastPicked, _ = os.ReadFile(last)
	}

	for n := max(1, m.Picked-historyLength+1); n <= m.Picked; n++ {
		if picture, err := os.ReadFile(s.pickedPath(n)); err == nil {
			if thumb, err := thumbnail(picture); err == nil {
				s.history = append(s.history, thumb)
			}
		}
	}

	return s, s.save()
}

func (s *session) save() error {
	raw, err := json.MarshalIndent(s.meta, "", "  ")
	if err != nil {
		return err
	}

	tmp := filepath.Join(s.dir, "meta.json.tmp")
	if err = os.WriteFile(tmp, raw, 0o640); err != nil {
		return err
	}

	return os.Rename(tmp, filepath.Join(s.dir, "meta.json"))
}

func loadMeta(dir string) (meta, error) {
	raw, err := os.ReadFile(filepath.Join(dir, "meta.json"))
	if err != nil {
		return meta{}, err
	}

	var m meta
	err = json.Unmarshal(raw, &m)

	return m, err
}

func (s *session) pickedPath(n int) string {
	return filepath.Join(s.dir, "picked", fmt.Sprintf("%06d.jpg", n))
}

// add keeps one frame of the current layer. A layer longer than max frames is
// thinned to every other frame, and from then on only every stride-th frame is
// kept, so a slow layer costs bounded memory and disk.
func (s *session) add(f frame) error {
	s.seen++
	if (s.seen-1)%s.stride != 0 {
		return nil
	}

	thumb, err := thumbnail(f.jpeg)
	if err != nil {
		return fmt.Errorf("a frame is not a readable JPEG: %w", err)
	}

	s.serial++
	file := filepath.Join(s.dir, "layer", fmt.Sprintf("%08d.jpg", s.serial))

	if err = os.WriteFile(file, f.jpeg, 0o640); err != nil {
		return err
	}

	s.layer = append(s.layer, buffered{at: f.at, thumb: thumb, file: file})

	if len(s.layer) > s.max {
		kept := s.layer[:0]

		start := s.layer[0].at

		for i, b := range s.layer {
			if i%2 == 0 || s.inParkWindow(start, b.at) {
				kept = append(kept, b)
			} else {
				_ = os.Remove(b.file)
			}
		}

		s.layer = kept
		s.stride *= 2
	}

	return nil
}

// closeLayer ends the layer at boundary: of the frames taken before it, the
// clearest is kept and the rest are deleted. Frames from after the boundary
// stay for the next layer. It answers whether a frame was kept.
func (s *session) closeLayer(boundary time.Time) (bool, error) {
	cut := len(s.layer)
	if !boundary.IsZero() {
		cut = 0
		for cut < len(s.layer) && s.layer[cut].at.Before(boundary) {
			cut++
		}
	}

	closing, rest := s.layer[:cut], s.layer[cut:]
	s.layer = append([]buffered(nil), rest...)
	s.stride, s.seen = 1, len(s.layer)
	s.meta.Layers++

	if len(closing) == 0 {
		return false, s.save()
	}

	thumbs := make([][]uint8, len(closing))
	for i, b := range closing {
		thumbs[i] = b.thumb
	}

	verdict := judge(thumbs, s.history)

	if s.park {
		if parked := pickParked(closing, s.parkWindow); parked >= 0 {
			verdict.best = parked
		}
	}

	best := verdict.best

	if s.diagnosing() {
		if err := s.keepCandidates(closing, verdict, len(s.history)); err != nil {
			fmt.Fprintf(os.Stderr, "%s: the candidates of layer %d were not kept: %v\n", name, s.meta.Layers, err)
		}
	}

	s.history = append(s.history, closing[best].thumb)
	if len(s.history) > historyLength {
		s.history = s.history[len(s.history)-historyLength:]
	}

	for i, b := range closing {
		if i != best {
			_ = os.Remove(b.file)
		}
	}

	s.meta.Picked++
	target := s.pickedPath(s.meta.Picked)

	if err := os.Rename(closing[best].file, target); err != nil {
		s.meta.Picked--

		return false, err
	}

	s.lastPicked, _ = os.ReadFile(target)

	return true, s.save()
}

// render turns the chosen frames into an H.264 MP4 that every browser plays.
// renderThreads bounds ffmpeg while it encodes. Left alone, x264 starts a
// thread for every core and holds a frame for each, which on a server with
// many cores takes the module over its memory_mib and gets it stopped.
const renderThreads = 2

// renderProfile is how ffmpeg encodes: threads and the widest frame. The low
// profile encodes with one thread at 1280 pixels and lower priority.
func renderProfile(settings Settings) (threads, width int) {
	if settings.low() {
		return 1, lowWidth
	}

	return renderThreads, 1920
}

func (s *session) render(ctx context.Context, settings Settings) (string, error) {
	if s.meta.Picked == 0 {
		return "", errors.New("no frame was kept")
	}

	out := filepath.Join(s.dir, "zeitraffer.mp4")
	threads, width := renderProfile(settings)

	// At most width wide and even on both sides, which yuv420p needs.
	filters := "scale=trunc(min(" + strconv.Itoa(width) + "\\,iw)/2)*2:-2"

	if settings.HoldLastS > 0 {
		filters += ",tpad=stop_mode=clone:stop_duration=" + strconv.FormatFloat(settings.HoldLastS, 'f', -1, 64)
	}

	base := []string{
		"-hide_banner", "-loglevel", "error", "-nostdin", "-y",
		"-filter_threads", "1",
		"-framerate", strconv.Itoa(settings.VideoFPS),
		"-i", filepath.Join(s.dir, "picked", "%06d.jpg"),
		"-vf", filters,
		"-pix_fmt", "yuv420p", "-movflags", "+faststart",
	}

	// libx264 is in nearly every ffmpeg build. A build without it falls back
	// to ffmpeg's own choice for MP4, which a browser may not play.
	attempts := [][]string{
		{"-c:v", "libx264", "-preset", "veryfast", "-crf", "23", "-threads", strconv.Itoa(threads)},
		{"-threads", strconv.Itoa(threads)},
	}

	var lastErr error

	for _, codec := range attempts {
		args := append(append(append([]string{}, base...), codec...), out)
		// ffmpeg comes from the admin-only setting `ffmpeg`, no shell is involved and the
		// arguments are fixed. Reviewed: no shell, admin-only path.
		cmd := exec.CommandContext(ctx, settings.FFmpeg, args...) // nosemgrep: go.lang.security.audit.dangerous-exec-command.dangerous-exec-command, go.lang.security.audit.dangerous-exec-command

		var stderr bytes.Buffer
		cmd.Stderr = &stderr

		err := cmd.Start()
		if err == nil {
			if settings.low() {
				lowerPriority(cmd.Process.Pid)
			}

			err = cmd.Wait()
		}

		if err != nil {
			lastErr = fmt.Errorf("ffmpeg: %w: %s", err, strings.TrimSpace(stderr.String()))

			continue
		}

		return out, nil
	}

	return "", lastErr
}

// preview answers the newest chosen frame, scaled down so it fits the host's
// one-megabyte limit on a module's answer.
func (s *session) preview() []byte {
	return scaledJPEG(s.lastPicked, 960)
}

func scaledJPEG(picture []byte, width int) []byte {
	if len(picture) == 0 {
		return nil
	}

	img, err := jpeg.Decode(bytes.NewReader(picture))
	if err != nil {
		return nil
	}

	bounds := img.Bounds()
	if bounds.Dx() > width {
		img = downscale(img, width)
	}

	var out bytes.Buffer
	if err = jpeg.Encode(&out, img, &jpeg.Options{Quality: 80}); err != nil {
		return nil
	}

	return out.Bytes()
}

// downscale is a box filter, plenty for a preview.
func downscale(img image.Image, width int) image.Image {
	bounds := img.Bounds()
	height := bounds.Dy() * width / bounds.Dx()
	out := image.NewRGBA(image.Rect(0, 0, width, height))

	for y := range height {
		sy0, sy1 := y*bounds.Dy()/height, (y+1)*bounds.Dy()/height

		for x := range width {
			sx0, sx1 := x*bounds.Dx()/width, (x+1)*bounds.Dx()/width

			var r, g, b, n uint64

			for sy := sy0; sy < max(sy1, sy0+1); sy++ {
				for sx := sx0; sx < max(sx1, sx0+1); sx++ {
					cr, cg, cb, _ := img.At(bounds.Min.X+sx, bounds.Min.Y+sy).RGBA()
					r, g, b, n = r+uint64(cr), g+uint64(cg), b+uint64(cb), n+1
				}
			}

			i := out.PixOffset(x, y)
			out.Pix[i], out.Pix[i+1], out.Pix[i+2], out.Pix[i+3] =
				uint8(r/n>>8), uint8(g/n>>8), uint8(b/n>>8), 0xFF
		}
	}

	return out
}

var unsafeName = regexp.MustCompile(`[^a-z0-9._-]+`)

// slug turns a printer or file name into a folder name every storage takes.
func slug(name string) string {
	lower := strings.ToLower(strings.TrimSpace(name))
	lower = strings.NewReplacer("ä", "ae", "ö", "oe", "ü", "ue", "ß", "ss").Replace(lower)
	lower = strings.TrimSuffix(strings.TrimSuffix(lower, ".bgcode"), ".gcode")
	out := strings.Trim(unsafeName.ReplaceAllString(lower, "-"), "-.")

	if len(out) > 60 {
		out = strings.TrimRight(out[:60], "-.")
	}

	if out == "" {
		return "unbenannt"
	}

	return out
}

// slicerSuffix matches where PrusaSlicer's default file name turns from the
// model's name into the print settings: the nozzle, as in "_0.4n_0.1mm_PETG_…".
var slicerSuffix = regexp.MustCompile(`(?i)[_ ]\d+(\.\d+)?n[_ ].*$`)

// shortPrintName is the print's name without the slicer's settings and
// extension, cut at a word boundary to at most 40 characters. The date in
// front of it already tells two runs of the same model apart.
func shortPrintName(name string) string {
	base := strings.TrimSuffix(strings.TrimSuffix(strings.TrimSpace(name), ".bgcode"), ".gcode")
	if cut := slicerSuffix.ReplaceAllString(base, ""); cut != "" {
		base = cut
	}

	out := slug(base)
	if len(out) > 40 {
		out = out[:40]
		if i := strings.LastIndexAny(out, "-_"); i >= 20 {
			out = out[:i]
		}

		out = strings.TrimRight(out, "-._")
	}

	return out
}

// runFolder is where one print's results go inside the storage.
func runFolder(settings Settings, m meta) string {
	printer := slug(m.DeviceName)
	if printer == "unbenannt" {
		printer = slug(m.DeviceID)
	}

	run := m.Started.In(settings.zone()).Format("2006-01-02_15-04") + "_" + shortPrintName(m.DisplayName)

	return strings.TrimPrefix(settings.Folder+"/"+printer+"/"+run, "/")
}

// printerFolder is the folder holding every run of one printer.
func printerFolder(settings Settings, name, id string) string {
	printer := slug(name)
	if printer == "unbenannt" {
		printer = slug(id)
	}

	return strings.TrimPrefix(settings.Folder+"/"+printer, "/")
}
