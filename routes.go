package main

import (
	"encoding/json"
	"net/http"
)

// routes is the module's HTTP surface under /api/v1/modules/<namespace>/.
// The host authorizes every caller before a request arrives here.
func (r *recorder) routes() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /status", func(w http.ResponseWriter, req *http.Request) {
		r.mu.Lock()
		status := r.status
		r.mu.Unlock()

		device := req.URL.Query().Get("device")
		status.ThisPrinter = status.Configured && (status.DeviceID == "" || status.DeviceID == device)

		if !status.ThisPrinter {
			// Another printer's tab shows nothing of this one's print.
			status = Status{Configured: status.Configured, DeviceID: status.DeviceID,
				DeviceName: status.DeviceName, Storage: status.Storage}
		}

		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(status)
	})

	mux.HandleFunc("GET /preview", func(w http.ResponseWriter, _ *http.Request) {
		r.mu.Lock()
		picture := r.preview
		r.mu.Unlock()

		if len(picture) == 0 {
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
