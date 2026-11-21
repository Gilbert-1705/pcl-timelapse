package main

import (
	// Die Zeitzonen stecken im Programm: das Image hat keine.
	_ "time/tzdata"

	"errors"
	"fmt"
	"net/url"
	"path"
	"strings"
	"time"
)

// Settings is the module's configuration as module.yaml declares it. The
// host fills in every default and sends the result on plugin.configure.
type Settings struct {
	CameraURL      string  `json:"camera_url"`
	CameraUsername string  `json:"camera_username"`
	CameraPassword string  `json:"camera_password"`
	Printer        string  `json:"printer"`
	Storage        string  `json:"storage"`
	Folder         string  `json:"folder"`
	CaptureFPS     float64 `json:"capture_fps"`
	VideoFPS       int     `json:"video_fps"`
	HoldLastS      float64 `json:"hold_last_s"`
	LayerSettle    string  `json:"layer_settle"`
	MinLayers      int     `json:"min_layers"`
	MaxPerLayer    int     `json:"max_frames_per_layer"`
	KeepFrames     bool    `json:"keep_frames"`
	FFmpeg         string  `json:"ffmpeg"`
	Timezone       string  `json:"timezone"`
	DiagnoseLayers int     `json:"diagnose_layers"`
	DiagnoseEvery  int     `json:"diagnose_every"`

	settle   time.Duration
	location *time.Location
}

var errSettings = errors.New("the timelapse settings are not usable")

// check validates the settings and fills in what is derived from them.
func (s *Settings) check() error {
	if s.CaptureFPS <= 0 || s.CaptureFPS > 10 {
		return fmt.Errorf("%w: capture_fps is %v and has to be above 0 and at most 10", errSettings, s.CaptureFPS)
	}

	if s.VideoFPS < 1 || s.VideoFPS > 120 {
		return fmt.Errorf("%w: video_fps is %d and has to be 1 to 120", errSettings, s.VideoFPS)
	}

	if s.MaxPerLayer < 8 {
		return fmt.Errorf("%w: max_frames_per_layer is %d and has to be at least 8", errSettings, s.MaxPerLayer)
	}

	if s.HoldLastS < 0 || s.HoldLastS > 30 {
		return fmt.Errorf("%w: hold_last_s is %v and has to be 0 to 30", errSettings, s.HoldLastS)
	}

	settle, err := time.ParseDuration(s.LayerSettle)
	if err != nil || settle < 2*time.Second || settle > 5*time.Minute {
		return fmt.Errorf("%w: layer_settle is %q and has to be a duration from 2s to 5m", errSettings, s.LayerSettle)
	}

	s.settle = settle

	s.location = time.Local
	if tz := strings.TrimSpace(s.Timezone); tz != "" {
		loc, err := time.LoadLocation(tz)
		if err != nil {
			return fmt.Errorf("%w: timezone %q is not a known time zone", errSettings, tz)
		}

		s.location = loc
	}

	if s.DiagnoseLayers < 0 || s.DiagnoseLayers > 50 {
		return fmt.Errorf("%w: diagnose_layers is %d and has to be 0 to 50", errSettings, s.DiagnoseLayers)
	}

	if s.DiagnoseEvery < 1 {
		s.DiagnoseEvery = 20
	}

	if strings.TrimSpace(s.Storage) == "" {
		return fmt.Errorf("%w: storage names no storage", errSettings)
	}

	s.Folder = strings.Trim(path.Clean("/"+strings.TrimSpace(s.Folder)), "/")

	if s.FFmpeg == "" {
		s.FFmpeg = "ffmpeg"
	}

	if s.CameraURL == "" {
		return nil
	}

	address, err := url.Parse(s.CameraURL)
	if err != nil || address.Host == "" {
		return fmt.Errorf("%w: camera_url is not an address", errSettings)
	}

	if s.CameraUsername != "" {
		address.User = url.UserPassword(s.CameraUsername, s.CameraPassword)
		s.CameraURL = address.String()
	}

	return nil
}

// configured reports whether a camera is set up at all. A module without one
// runs, says so on its tab and records nothing.
func (s Settings) configured() bool { return s.CameraURL != "" }

// zone is where dates in folder names are told, the server's own zone when
// none is set.
func (s Settings) zone() *time.Location {
	if s.location == nil {
		return time.Local
	}

	return s.location
}
