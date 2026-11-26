package main

import (
	"bytes"
	"image/jpeg"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// A head that moves on every frame except for three, while it is parked.
func TestTheParkedFrameIsTheStillestAtTheStartOfTheLayer(t *testing.T) {
	start := time.Date(2026, 11, 22, 10, 0, 0, 0, time.UTC)

	var frames []buffered

	for i := range 40 {
		thumb := make([]uint8, thumbWidth*thumbHeight)

		head := i * 4 % thumbWidth
		if i >= 5 && i <= 7 {
			head = -1 // parked out of view
		}

		for p := range thumb {
			thumb[p] = 100
			if head >= 0 && p%thumbWidth >= head && p%thumbWidth < head+20 {
				thumb[p] = 20
			}
		}

		frames = append(frames, buffered{at: start.Add(time.Duration(i) * 500 * time.Millisecond), thumb: thumb})
	}

	if got := pickParked(frames, 10*time.Second); got != 6 {
		t.Fatalf("picked %d, want the middle of the park, 6", got)
	}

	// The park is outside a window of two seconds (frames 0 to 3).
	if got := pickParked(frames, 2*time.Second); got == 6 {
		t.Fatal("a frame outside the window was picked")
	}

	if got := pickParked(frames[:2], 10*time.Second); got != -1 {
		t.Fatalf("two frames gave %d, want -1", got)
	}
}

func TestSettingsReadThePerformanceProfileAndTheParkWindow(t *testing.T) {
	base := Settings{
		CaptureFPS: 2, VideoFPS: 30, LayerSettle: "12s", MinLayers: 10, MaxPerLayer: 240,
		Storage: "local", Folder: "zeitraffer",
	}

	s := base
	if err := s.check(); err != nil || s.Performance != performanceNormal || s.parkWindow != 20*time.Second {
		t.Fatalf("defaults: %v, %q, %v", err, s.Performance, s.parkWindow)
	}

	s = base
	s.Performance, s.ParkWindow = " LOW ", "8s"
	if err := s.check(); err != nil || !s.low() || s.captureFPS() != 1 || s.parkWindow != 8*time.Second {
		t.Fatalf("low: %v, %v, %v, %v", err, s.low(), s.captureFPS(), s.parkWindow)
	}

	for _, bad := range []Settings{
		func() Settings { b := base; b.Performance = "turbo"; return b }(),
		func() Settings { b := base; b.ParkWindow = "1s"; return b }(),
		func() Settings { b := base; b.ParkWindow = "soon"; return b }(),
	} {
		if err := bad.check(); err == nil {
			t.Errorf("%q / %q was accepted", bad.Performance, bad.ParkWindow)
		}
	}

	if threads, width := renderProfile(Settings{Performance: performanceLow}); threads != 1 || width != lowWidth {
		t.Fatalf("low render profile: %d threads, %d wide", threads, width)
	}
}

// The low profile's ffmpeg arguments, run against a file with a keyframe
// every half second: at most one frame a second, none repeated, 1280 wide.
func TestTheLowProfileReadsOnlyKeyframesOnceASecond(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("no ffmpeg")
	}

	dir := t.TempDir()
	video := filepath.Join(dir, "camera.mp4")

	encode := exec.Command("ffmpeg", "-loglevel", "error", "-y", "-f", "lavfi", "-i", "testsrc=size=1920x1080:rate=10",
		"-t", "6", "-c:v", "libx264", "-g", "5", "-keyint_min", "5", "-sc_threshold", "0", video)
	if out, err := encode.CombinedOutput(); err != nil {
		t.Skipf("no H.264 encoder: %v %s", err, out)
	}

	args := ffmpegSource{url: video, fps: 1, low: true}.captureArgs()
	if !slices.Contains(args, "nokey") || !slices.Contains(args, "vfr") {
		t.Fatalf("arguments: %v", args)
	}

	// A file gets no RTSP options (inputArgs); write single files.
	if slices.Contains(args, "-rtsp_transport") {
		t.Fatalf("a file gets RTSP options: %v", args)
	}

	args = append(args[:len(args)-3], "-f", "image2", filepath.Join(dir, "f%03d.jpg"))

	if out, err := exec.Command("ffmpeg", args...).CombinedOutput(); err != nil {
		t.Fatalf("ffmpeg: %v %s", err, out)
	}

	entries, _ := os.ReadDir(dir)

	var shots []string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "f") {
			shots = append(shots, e.Name())
		}
	}

	if len(shots) < 5 || len(shots) > 7 {
		t.Fatalf("%d frames from 6 seconds, want about 6", len(shots))
	}

	raw, err := os.ReadFile(filepath.Join(dir, shots[0]))
	if err != nil {
		t.Fatal(err)
	}

	if img, err := jpeg.Decode(bytes.NewReader(raw)); err != nil || img.Bounds().Dx() != lowWidth {
		t.Fatalf("first frame: %v, %v", err, img)
	}
}
