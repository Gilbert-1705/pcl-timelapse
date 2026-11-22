package main

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"
)

// A server without ffmpeg is told so in words an operator can act on, and not
// with the bare error of exec.
func TestAMissingFFmpegIsNamedPlainly(t *testing.T) {
	for _, path := range []string{"", "/nonexistent/ffmpeg", "ffmpeg-that-does-not-exist"} {
		_, err := checkFFmpeg(context.Background(), path)
		if !errors.Is(err, errFFmpegMissing) {
			t.Fatalf("%q: want errFFmpegMissing, got %v", path, err)
		}

		if !strings.Contains(err.Error(), "ffmpeg") || strings.Contains(err.Error(), "$PATH") {
			t.Fatalf("%q: the message does not read plainly: %v", path, err)
		}
	}
}

// A program that is there but is no ffmpeg (it exits with an error) is
// refused too.
func TestAProgramThatFailsIsNotTakenForFFmpeg(t *testing.T) {
	bin, err := exec.LookPath("false")
	if err != nil {
		t.Skip("no false on this system")
	}

	if _, err = checkFFmpeg(context.Background(), bin); !errors.Is(err, errFFmpegMissing) {
		t.Fatalf("want errFFmpegMissing, got %v", err)
	}
}

func TestAnInstalledFFmpegReportsItsVersion(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg is not installed here")
	}

	version, err := checkFFmpeg(context.Background(), "ffmpeg")
	if err != nil || !strings.HasPrefix(version, "ffmpeg version") {
		t.Fatalf("version %q, err %v", version, err)
	}
}

// The tab reads the problem from the status before the first print, and a
// later check with ffmpeg in place clears it.
func TestTheStatusNamesAMissingFFmpegAndClearsItOnceThere(t *testing.T) {
	r := newRecorder()
	r.settings = Settings{FFmpeg: "/nonexistent/ffmpeg"}

	if r.checkFFmpeg(context.Background()) {
		t.Fatal("a missing ffmpeg was taken as usable")
	}

	if r.status.FFmpegProblem == "" {
		t.Fatal("the status does not name the problem")
	}

	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg is not installed here")
	}

	r.settings.FFmpeg = "ffmpeg"
	if !r.checkFFmpeg(context.Background()) || r.status.FFmpegProblem != "" || r.status.FFmpeg == "" {
		t.Fatalf("status after installing ffmpeg: %+v", r.status)
	}
}
