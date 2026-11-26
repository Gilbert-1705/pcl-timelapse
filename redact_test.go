package main

import (
	"bytes"
	"net/url"
	"os/exec"
	"strings"
	"testing"
)

// SEC-0001: ffmpeg quotes the address with the password in userinfo escaping.
// No spelling of the password, and no userinfo, may reach the log.
func TestTheCameraPasswordNeverReachesTheLog(t *testing.T) {
	passwords := []string{
		"plain", "geheim wort!", "with#hash", "100%sure", "a@b", "co:lon",
		"sl/ash", "Grüße-ÄÖÜ", "ä ö ü ß", "q?u&e=ry", "+plus+",
	}

	for _, password := range passwords {
		address := (&url.URL{Scheme: "rtsp", User: url.UserPassword("viewer", password),
			Host: "192.0.2.50:8554", Path: "/live"}).String()

		lines := []string{
			"Error opening input file " + address + ".",
			"[rtsp @ 0x5581] method DESCRIBE failed: 401 Unauthorized for " + address,
			"password was " + password,
			"query form " + url.QueryEscape(password),
		}

		var out bytes.Buffer
		log := &redactedLog{out: &out, prefix: "ffmpeg: ", secrets: secretsOf(address)}

		for _, line := range lines {
			// Split every line across two writes, as a pipe may.
			half := len(line) / 2
			_, _ = log.Write([]byte(line[:half]))
			_, _ = log.Write([]byte(line[half:] + "\n"))
		}

		log.Flush()

		written := out.String()
		for _, spelling := range []string{
			password, url.QueryEscape(password), url.PathEscape(password),
			strings.TrimPrefix(url.UserPassword("", password).String(), ":"),
		} {
			if len(spelling) >= 3 && strings.Contains(written, spelling) {
				t.Errorf("password %q: %q is in the log:\n%s", password, spelling, written)
			}
		}

		if strings.Contains(written, "viewer") {
			t.Errorf("password %q: the username is in the log:\n%s", password, written)
		}

		if strings.Count(written, "\n") != len(lines) || !strings.Contains(written, "rtsp://xxxxx@192.0.2.50:8554/live") {
			t.Errorf("password %q: the lines are not kept intact:\n%s", password, written)
		}
	}
}

func TestALogWithoutCredentialsIsLeftAlone(t *testing.T) {
	var out bytes.Buffer
	log := &redactedLog{out: &out, prefix: "ffmpeg: ", secrets: secretsOf("rtsp://192.0.2.50/live")}

	_, _ = log.Write([]byte("Error opening input file rtsp://192.0.2.50/live.\nlast line without newline"))
	log.Flush()

	want := "timelapse: ffmpeg: Error opening input file rtsp://192.0.2.50/live.\n" +
		"timelapse: ffmpeg: last line without newline\n"
	if out.String() != want {
		t.Fatalf("log:\n%s\nwant:\n%s", out.String(), want)
	}
}

// The same with ffmpeg itself: it cannot reach the address and says so,
// address included.
func TestFFmpegsOwnErrorIsRedacted(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("no ffmpeg")
	}

	address := (&url.URL{Scheme: "rtsp", User: url.UserPassword("viewer", "geheim wort!"),
		Host: "127.0.0.1:1", Path: "/live"}).String()
	source := ffmpegSource{ffmpeg: ffmpeg, url: address, fps: 1}

	var out bytes.Buffer
	log := &redactedLog{out: &out, prefix: "ffmpeg: ", secrets: secretsOf(address)}

	cmd := exec.Command(ffmpeg, source.captureArgs()...)
	cmd.Stderr = log
	_ = cmd.Run()
	log.Flush()

	written := out.String()
	if written == "" || strings.Contains(written, "geheim") || strings.Contains(written, "viewer") {
		t.Fatalf("ffmpeg's log:\n%s", written)
	}
}
