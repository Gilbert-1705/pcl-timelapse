package main

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/prusa3d/prusa-connect/sdk/extension"
)

var t0 = time.Date(2026, 11, 20, 20, 0, 0, 0, time.UTC)

func at(s float64) time.Time { return t0.Add(time.Duration(s * float64(time.Second))) }

func TestLayerTrackerConfirmsARiseThatHolds(t *testing.T) {
	l := newLayerTracker(10 * time.Second)
	l.reset(at(0), 0.2, true)

	l.observe(at(30), 0.4)

	if _, ok := l.tick(at(35)); ok {
		t.Fatal("a rise was confirmed before it held for the settle time")
	}

	boundary, ok := l.tick(at(41))
	if !ok || !boundary.Equal(at(30)) {
		t.Fatalf("boundary %v %v, want the moment of the rise", boundary, ok)
	}

	if l.height() != 0.4 {
		t.Fatalf("height %v, want 0.4", l.height())
	}
}

func TestLayerTrackerIgnoresAZHop(t *testing.T) {
	l := newLayerTracker(10 * time.Second)
	l.reset(at(0), 1.0, true)

	l.observe(at(5), 1.6) // caught mid-hop
	l.observe(at(10), 1.0)

	if _, ok := l.tick(at(30)); ok {
		t.Fatal("a Z hop was taken for a layer")
	}
}

func TestLayerTrackerKeepsTheFirstRiseThroughAHopOnTheNewLayer(t *testing.T) {
	l := newLayerTracker(10 * time.Second)
	l.reset(at(0), 1.0, true)

	l.observe(at(20), 1.2) // the new layer
	l.observe(at(25), 1.8) // a hop on it
	l.observe(at(28), 1.2) // back

	boundary, ok := l.tick(at(31))
	if !ok || !boundary.Equal(at(20)) || l.height() != 1.2 {
		t.Fatalf("boundary %v %v height %v", boundary, ok, l.height())
	}
}

func TestLayerTrackerCorrectsAConfirmedHop(t *testing.T) {
	l := newLayerTracker(5 * time.Second)
	l.reset(at(0), 1.0, true)

	l.observe(at(10), 1.2)
	l.observe(at(11), 1.8) // hop, and the next sample comes late
	l.tick(at(16))

	l.observe(at(18), 1.2)

	if l.height() != 1.2 {
		t.Fatalf("height %v, want the floor back at 1.2", l.height())
	}

	if _, ok := l.tick(at(40)); ok {
		t.Fatal("the correction made a second boundary")
	}
}

// scene draws a bed with a growing object and the print head at x.
func scene(headX int) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, 640, 360))

	for y := range 360 {
		for x := range 640 {
			c := color.RGBA{60, 60, 70, 255}
			if x > 260 && x < 380 && y > 200 && y < 300 {
				c = color.RGBA{230, 120, 30, 255} // the print
			}

			if x > headX && x < headX+90 && y > 120 && y < 280 {
				c = color.RGBA{20, 20, 20, 255} // the head
			}

			img.Set(x, y, c)
		}
	}

	return img
}

func encode(t *testing.T, img image.Image) []byte {
	t.Helper()

	var out bytes.Buffer
	if err := jpeg.Encode(&out, img, &jpeg.Options{Quality: 85}); err != nil {
		t.Fatal(err)
	}

	return out.Bytes()
}

func thumbsAt(t *testing.T, positions []int) [][]uint8 {
	t.Helper()

	thumbs := make([][]uint8, len(positions))

	for i, x := range positions {
		thumb, err := thumbnail(encode(t, scene(x)))
		if err != nil {
			t.Fatal(err)
		}

		thumbs[i] = thumb
	}

	return thumbs
}

// The head spends most of the layer above the print. The layer alone cannot
// tell, but the frames kept for earlier layers show the print uncovered.
func TestPickClearestUsesTheKeptFramesWhenTheHeadStaysOverThePrint(t *testing.T) {
	positions := []int{250, 280, 300, 220, 540, 270, 310, 240, 290}
	history := thumbsAt(t, []int{540, 20, 560, 30, 500})

	if got := pickClearest(thumbsAt(t, positions), history); positions[got] != 540 {
		t.Fatalf("picked the frame with the head at %d", positions[got])
	}
}

// Without history, the layer's own median decides, which works while the
// head moves about.
func TestPickClearestWithoutHistory(t *testing.T) {
	positions := []int{100, 280, 450, 20, 540, 180, 330, 60, 500}

	got := positions[pickClearest(thumbsAt(t, positions), nil)]
	if got+90 > 260 && got < 380 {
		t.Fatalf("picked the frame with the head at %d, over the print", got)
	}
}

func TestSplitJPEGsReadsFfmpegsPipe(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("no ffmpeg")
	}

	out, err := exec.Command(ffmpeg, "-hide_banner", "-loglevel", "error",
		"-f", "lavfi", "-i", "testsrc=size=1280x720:rate=10", "-frames:v", "7",
		"-c:v", "mjpeg", "-q:v", "3", "-f", "image2pipe", "-").Output()
	if err != nil {
		t.Fatal(err)
	}

	var n int

	_ = splitJPEGs(bytes.NewReader(out), func(picture []byte) {
		if _, err := jpeg.Decode(bytes.NewReader(picture)); err != nil {
			t.Fatalf("frame %d does not decode: %v", n, err)
		}
		n++
	})

	if n != 7 {
		t.Fatalf("split %d frames, want 7", n)
	}
}

func TestASessionKeepsOneFramePerLayerAndRendersAVideo(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("no ffmpeg")
	}

	dir := t.TempDir()

	s, err := openSession(dir, meta{JobID: "j1", DeviceName: "Core One", DisplayName: "Würfel.bgcode", Started: t0},
		10*time.Second, 16)
	if err != nil {
		t.Fatal(err)
	}

	for layer := range 5 {
		for i, x := range []int{250, 540, 280, 300} {
			when := at(float64(layer*60 + i*10))
			if err = s.add(frame{at: when, jpeg: encode(t, scene(x))}); err != nil {
				t.Fatal(err)
			}
		}

		if _, err = s.closeLayer(at(float64(layer*60 + 59))); err != nil {
			t.Fatal(err)
		}
	}

	if s.meta.Picked != 5 || s.meta.Layers != 5 {
		t.Fatalf("picked %d of %d layers", s.meta.Picked, s.meta.Layers)
	}

	left, _ := filepath.Glob(filepath.Join(dir, "layer", "*.jpg"))
	if len(left) != 0 {
		t.Fatalf("%d frames were left behind", len(left))
	}

	settings := Settings{VideoFPS: 30, HoldLastS: 1, FFmpeg: "ffmpeg"}

	video, err := s.render(context.Background(), settings)
	if err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(video)
	if err != nil || info.Size() == 0 {
		t.Fatalf("no video: %v", err)
	}

	if p := s.preview(); len(p) == 0 || len(p) > 1<<20 {
		t.Fatalf("preview of %d bytes", len(p))
	}
}

func TestALongLayerIsThinned(t *testing.T) {
	s, err := openSession(t.TempDir(), meta{JobID: "j"}, time.Second, 8)
	if err != nil {
		t.Fatal(err)
	}

	picture := encode(t, scene(540))

	for i := range 40 {
		if err = s.add(frame{at: at(float64(i)), jpeg: picture}); err != nil {
			t.Fatal(err)
		}
	}

	if len(s.layer) > 8 {
		t.Fatalf("%d frames held, the limit is 8", len(s.layer))
	}

	files, _ := filepath.Glob(filepath.Join(s.dir, "layer", "*.jpg"))
	if len(files) != len(s.layer) {
		t.Fatalf("%d files for %d frames", len(files), len(s.layer))
	}
}

func TestFoldersAreSafeNames(t *testing.T) {
	settings := Settings{Folder: "zeitraffer"}
	m := meta{DeviceName: "Prusa Core One", DisplayName: "Halter für Ständer v2.bgcode",
		Started: time.Date(2026, 11, 20, 18, 5, 0, 0, time.Local)}

	got := runFolder(settings, m)
	want := "zeitraffer/prusa-core-one/2026-11-20_18-05_halter-fuer-staender-v2"

	if got != want {
		t.Fatalf("%q, want %q", got, want)
	}
}

func TestFolderDropsTheSlicerSettings(t *testing.T) {
	settings := Settings{Folder: "zeitraffer"}
	m := meta{DeviceName: "Core One",
		DisplayName: "Cable_Clip_Set_Large_Version_With_Labels_V2_0.4n_0.2mm_PETG_COREONE_1h12m.bgcode",
		Started:     time.Date(2026, 11, 20, 23, 44, 0, 0, time.Local)}

	got := runFolder(settings, m)
	want := "zeitraffer/core-one/2026-11-20_23-44_cable_clip_set_large_version_with"

	if got != want {
		t.Fatalf("%q, want %q", got, want)
	}
}

func TestSettingsPutTheCredentialsIntoTheAddress(t *testing.T) {
	s := Settings{CameraURL: "rtsp://192.0.2.50/live", CameraUsername: "cam", CameraPassword: "geh eim",
		Storage: "local", CaptureFPS: 2, VideoFPS: 30, MaxPerLayer: 240, LayerSettle: "12s"}

	if err := s.check(); err != nil {
		t.Fatal(err)
	}

	if s.CameraURL != "rtsp://cam:geh%20eim@192.0.2.50/live" {
		t.Fatalf("address %q", s.CameraURL)
	}

	hidden := secretsOf(s.CameraURL)
	if len(hidden) == 0 || hidden[0] != "geh eim" {
		t.Fatalf("secrets %q", hidden)
	}
}

// Prusa Connect Local names an extension after its module path, and every
// part has to be a DNS-1123 label: an upper-case letter makes the install fail.
func TestTheModulePathGivesANamespace(t *testing.T) {
	raw, err := os.ReadFile("go.mod")
	if err != nil {
		t.Fatal(err)
	}

	first, _, _ := strings.Cut(string(raw), "\n")
	module := strings.TrimSpace(strings.TrimPrefix(first, "module "))

	if _, err := extension.NamespaceOf(module); err != nil {
		t.Fatalf("module %q: %v", module, err)
	}
}
