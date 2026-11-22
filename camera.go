package main

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
)

// cameraPorts are the only ports the extension connects to, the same list
// module.yaml declares under egress for the setting camera_url. An address on
// another port is refused when the settings are read, so the declaration an
// operator approves is what the process does.
//
//	554, 8554  RTSP (8554 is common for cameras and RTSP relays)
//	322        RTSPS
//	80, 8080   HTTP snapshot addresses
//	443        HTTPS snapshot addresses
var cameraPorts = []int{554, 8554, 322, 80, 8080, 443}

// cameraPortAllowed reports whether the process may connect to port. Only
// tests replace it, to reach a fake camera on a random local port.
var cameraPortAllowed = func(port int) bool { return slices.Contains(cameraPorts, port) }

// cameraDefaultPorts is the port each scheme uses when the address names none.
var cameraDefaultPorts = map[string]int{"rtsp": 554, "rtsps": 322, "http": 80, "https": 443}

// checkCameraURL reads camera_url: one of the four schemes, a host, and a
// port from cameraPorts.
func checkCameraURL(raw string) error {
	address, err := url.Parse(raw)
	if err != nil || address.Host == "" || address.Hostname() == "" {
		return fmt.Errorf("%w: camera_url is not an address", errSettings)
	}

	scheme := strings.ToLower(address.Scheme)

	defaultPort, known := cameraDefaultPorts[scheme]
	if !known {
		return fmt.Errorf("%w: camera_url must start with rtsp://, rtsps://, http:// or https://", errSettings)
	}

	port := defaultPort

	if text := address.Port(); text != "" {
		if port, err = strconv.Atoi(text); err != nil {
			return fmt.Errorf("%w: camera_url has no valid port", errSettings)
		}
	}

	if !cameraPortAllowed(port) {
		return fmt.Errorf("%w: camera_url uses port %d; the extension only connects to the ports it declares (%s)",
			errSettings, port, portList())
	}

	return nil
}

func portList() string {
	parts := make([]string, 0, len(cameraPorts))
	for _, port := range cameraPorts {
		parts = append(parts, strconv.Itoa(port))
	}

	return strings.Join(parts, ", ")
}

// errRedirect refuses a snapshot address that points somewhere else.
var errRedirect = errors.New("the camera redirected to another address, which is not followed")

// cameraClient is the HTTP client for snapshot addresses. It follows a
// redirect only to the same scheme, host and port, so a camera cannot send the
// process, and the credentials in the address, to a host the operator did not
// set.
func cameraClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout: timeout,
		CheckRedirect: func(next *http.Request, via []*http.Request) error {
			first := via[0].URL
			if len(via) >= 3 || !strings.EqualFold(next.URL.Scheme, first.Scheme) || !strings.EqualFold(next.URL.Host, first.Host) {
				return errRedirect
			}

			return nil
		},
	}
}
