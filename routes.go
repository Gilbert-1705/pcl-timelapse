package main

import (
	"encoding/json"
	"net/http"

	"github.com/prusa3d/prusa-connect/sdk/routesapi"
)

// routes is the module's HTTP surface under /api/v1/modules/<namespace>/.
// The host lets in everyone who may open the extension; what they see of the
// printer the recorder follows depends on whether they may observe it
// (access.go).
func (r *recorder) routes() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /status", func(w http.ResponseWriter, req *http.Request) {
		r.mu.Lock()
		status := r.status
		r.mu.Unlock()

		if status.DeviceID != "" && !mayObserve(req.Context(), r.access, routesapi.PrincipalOf(req), status.DeviceID) {
			// Someone who may not see the printer learns nothing about it,
			// not even its name. ffmpeg's state is about the server.
			status = Status{Configured: status.Configured, Storage: status.Storage,
				FFmpeg: status.FFmpeg, FFmpegProblem: status.FFmpegProblem}
			writeStatus(w, status)

			return
		}

		device := req.URL.Query().Get("device")
		status.ThisPrinter = status.Configured && (status.DeviceID == "" || status.DeviceID == device)

		if !status.ThisPrinter {
			// Another printer's tab shows nothing of this one's print.
			status = Status{Configured: status.Configured, DeviceID: status.DeviceID,
				DeviceName: status.DeviceName, Storage: status.Storage}
		}

		writeStatus(w, status)
	})

	mux.HandleFunc("GET /preview", func(w http.ResponseWriter, req *http.Request) {
		r.mu.Lock()
		picture := r.preview
		device := r.status.DeviceID
		r.mu.Unlock()

		// The picture shows the printer the recorder follows. Without that
		// printer or without the right to observe it there is nothing to show.
		if len(picture) == 0 || !mayObserve(req.Context(), r.access, routesapi.PrincipalOf(req), device) {
			w.WriteHeader(http.StatusNoContent)

			return
		}

		w.Header().Set("Content-Type", "image/jpeg")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "no-store")
		// A re-encoded JPEG served as image/jpeg with nosniff, not HTML.
		_, _ = w.Write(picture) // nosemgrep: go.lang.security.audit.xss.no-direct-write-to-responsewriter.no-direct-write-to-responsewriter, go.lang.security.audit.xss.no-direct-write-to-responsewriter
	})

	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})

	return mux
}

func writeStatus(w http.ResponseWriter, status Status) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(status)
}
