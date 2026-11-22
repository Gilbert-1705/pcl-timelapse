package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// ffmpegCheckTimeout bounds `ffmpeg -version`, which answers at once on any
// working install.
const ffmpegCheckTimeout = 10 * time.Second

// errFFmpegMissing marks a server without a usable ffmpeg. Prusa Connect Local
// does not ship ffmpeg, so the operator installs it next to the server.
var errFFmpegMissing = errors.New("ffmpeg is not available on this server")

// checkFFmpeg tells whether the ffmpeg named by the setting `ffmpeg` can run,
// and returns the first line of its version. It is asked when the extension
// starts and again before every recording, so installing ffmpeg later takes
// effect without a restart. The error says what to do in plain words.
func checkFFmpeg(ctx context.Context, path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", fmt.Errorf("%w: the setting ffmpeg is empty; set it to ffmpeg or to the full path of the program", errFFmpegMissing)
	}

	found, err := exec.LookPath(path)
	if err != nil {
		return "", fmt.Errorf("%w: %q was not found. Install ffmpeg where the server runs (for example apt install ffmpeg) "+
			"or set the setting ffmpeg to its full path", errFFmpegMissing, path)
	}

	ctx, cancel := context.WithTimeout(ctx, ffmpegCheckTimeout)
	defer cancel()

	var out bytes.Buffer

	// The path comes from the admin-only setting `ffmpeg` and was resolved by
	// LookPath above; no shell is involved and the only argument is fixed.
	cmd := exec.CommandContext(ctx, found, "-hide_banner", "-version") // nosemgrep: go.lang.security.audit.dangerous-exec-command.dangerous-exec-command, go.lang.security.audit.dangerous-exec-command
	cmd.Stdout = &out

	if err = cmd.Run(); err != nil {
		return "", fmt.Errorf("%w: %s did not run (%v). Check that it is an ffmpeg build for this system", errFFmpegMissing, found, err)
	}

	first, _, _ := strings.Cut(out.String(), "\n")

	return strings.TrimSpace(first), nil
}
