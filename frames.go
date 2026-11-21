package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// maxFrameBytes bounds one JPEG read from the camera. A Buddy3D frame at
// 1080p is a few hundred kilobytes.
const maxFrameBytes = 16 << 20

// frame is one picture and the moment it arrived.
type frame struct {
	at   time.Time
	jpeg []byte
}

// frameSource delivers frames until ctx ends.
type frameSource interface {
	run(ctx context.Context, out chan<- frame) error
}

// sourceFor picks how to read the camera from its address: an RTSP stream is
// decoded by ffmpeg, an HTTP address is polled as a snapshot URL.
func sourceFor(settings Settings) (frameSource, error) {
	address, err := url.Parse(settings.CameraURL)
	if err != nil {
		return nil, fmt.Errorf("camera_url is not an address: %w", err)
	}

	interval := time.Duration(float64(time.Second) / settings.CaptureFPS)

	switch strings.ToLower(address.Scheme) {
	case "rtsp", "rtsps":
		return ffmpegSource{ffmpeg: settings.FFmpeg, url: settings.CameraURL, fps: settings.CaptureFPS}, nil
	case "http", "https":
		return snapshotSource{url: settings.CameraURL, every: interval}, nil
	default:
		return nil, fmt.Errorf("camera_url %q: only rtsp://, rtsps://, http:// and https:// are read", address.Redacted())
	}
}

// ffmpegSource reads an RTSP stream through ffmpeg, which decodes the H.264
// stream and hands back fps JPEGs a second on its standard output.
type ffmpegSource struct {
	ffmpeg string
	url    string
	fps    float64
}

func (s ffmpegSource) run(ctx context.Context, out chan<- frame) error {
	args := []string{
		"-hide_banner", "-loglevel", "error", "-nostdin",
		"-threads", "2", "-filter_threads", "1",
		"-rtsp_transport", "tcp",
		"-i", s.url,
		"-an",
		"-vf", "fps=" + strconv.FormatFloat(s.fps, 'f', -1, 64),
		"-c:v", "mjpeg", "-q:v", "3",
		"-f", "image2pipe", "-",
	}

	// ffmpeg comes from the admin-only setting `ffmpeg`, no shell is involved and the
	// arguments are fixed. Reviewed: no shell, admin-only path.
	cmd := exec.CommandContext(ctx, s.ffmpeg, args...) // nosemgrep: go.lang.security.audit.dangerous-exec-command.dangerous-exec-command, go.lang.security.audit.dangerous-exec-command
	cmd.Stderr = prefixed{prefix: "ffmpeg: ", hide: secretsOf(s.url)}
	cmd.WaitDelay = 5 * time.Second

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}

	if err = cmd.Start(); err != nil {
		return fmt.Errorf("start %s: %w", s.ffmpeg, err)
	}

	readErr := splitJPEGs(stdout, func(jpeg []byte) {
		select {
		case out <- frame{at: time.Now(), jpeg: jpeg}:
		case <-ctx.Done():
		}
	})

	waitErr := cmd.Wait()
	if ctx.Err() != nil {
		return nil
	}

	if readErr != nil && !errors.Is(readErr, io.EOF) {
		return readErr
	}

	if waitErr != nil {
		return fmt.Errorf("ffmpeg ended: %w", waitErr)
	}

	return errors.New("ffmpeg ended without an error, the stream closed")
}

// splitJPEGs cuts a stream of concatenated JPEGs at their start and end
// markers. ffmpeg's MJPEG encoder writes no embedded thumbnails, so the first
// end marker after a start marker ends the picture.
func splitJPEGs(r io.Reader, each func([]byte)) error {
	in := bufio.NewReaderSize(r, 1<<20)

	var (
		current bytes.Buffer
		inside  bool
		prev    byte
	)

	for {
		b, err := in.ReadByte()
		if err != nil {
			return err
		}

		switch {
		case !inside && prev == 0xFF && b == 0xD8:
			inside = true

			current.Reset()
			current.Write([]byte{0xFF, 0xD8})
		case inside:
			current.WriteByte(b)

			if prev == 0xFF && b == 0xD9 {
				inside = false

				each(bytes.Clone(current.Bytes()))
			} else if current.Len() > maxFrameBytes {
				inside = false
			}
		}

		prev = b
	}
}

// snapshotSource polls a URL that answers one JPEG.
type snapshotSource struct {
	url   string
	every time.Duration
}

func (s snapshotSource) run(ctx context.Context, out chan<- frame) error {
	client := &http.Client{Timeout: 10 * time.Second}
	ticker := time.NewTicker(s.every)

	defer ticker.Stop()

	failures := 0

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}

		picture, err := fetchJPEG(ctx, client, s.url)
		if ctx.Err() != nil {
			return nil
		}

		if err != nil {
			failures++
			if failures == 1 || failures%30 == 0 {
				fmt.Fprintf(os.Stderr, "timelapse: snapshot: %v (%d in a row)\n", err, failures)
			}

			continue
		}

		failures = 0

		select {
		case out <- frame{at: time.Now(), jpeg: picture}:
		case <-ctx.Done():
			return nil
		}
	}
}

func fetchJPEG(ctx context.Context, client *http.Client, address string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return nil, err
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("the camera answered %s", resp.Status)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxFrameBytes+1))
	if err != nil {
		return nil, err
	}

	if len(body) > maxFrameBytes || len(body) < 4 || body[0] != 0xFF || body[1] != 0xD8 {
		return nil, errors.New("the camera did not answer a JPEG")
	}

	return body, nil
}

// prefixed writes another process's error output line by line to ours, where
// the host puts it into its log. ffmpeg repeats the address it was given in
// its errors, so the password in it is blanked first.
type prefixed struct {
	prefix string
	hide   []string
}

func (p prefixed) Write(b []byte) (int, error) {
	text := string(b)
	for _, secret := range p.hide {
		text = strings.ReplaceAll(text, secret, "xxxxx")
	}

	for line := range strings.SplitSeq(strings.TrimRight(text, "\n"), "\n") {
		if line != "" {
			fmt.Fprintf(os.Stderr, "timelapse: %s%s\n", p.prefix, line)
		}
	}

	return len(b), nil
}

// secretsOf answers the parts of an address that must not reach a log.
func secretsOf(address string) []string {
	parsed, err := url.Parse(address)
	if err != nil || parsed.User == nil {
		return nil
	}

	if password, set := parsed.User.Password(); set && password != "" {
		return []string{password, url.QueryEscape(password)}
	}

	return nil
}
