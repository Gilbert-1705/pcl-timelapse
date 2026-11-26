package main

import (
	"slices"
	"testing"
)

// The camera stream may only be read over RTSP: ffmpeg gets a protocol
// whitelist right before the address, in both profiles.
func TestTheStreamIsReadWithAProtocolWhitelist(t *testing.T) {
	for _, low := range []bool{false, true} {
		for _, address := range []string{"rtsp://192.0.2.50/live", "RTSPS://192.0.2.50:322/live"} {
			args := ffmpegSource{url: address, fps: 1, low: low}.captureArgs()

			at := slices.Index(args, "-i")
			if at < 2 || args[at+1] != address {
				t.Fatalf("low=%v %s: no input in %q", low, address, args)
			}

			if args[at-2] != "-protocol_whitelist" || args[at-1] != "rtsp,rtp,tcp,udp,tls" {
				t.Fatalf("low=%v %s: the input is not limited to RTSP: %q", low, address, args)
			}

			if !slices.Contains(args, "-rtsp_transport") {
				t.Fatalf("low=%v %s: RTSP no longer runs over TCP: %q", low, address, args)
			}
		}
	}
}

// Local files in the tests keep ffmpeg's defaults; they never reach a
// running module (sourceFor only hands RTSP addresses to ffmpeg).
func TestALocalFileIsReadWithoutTheRTSPOptions(t *testing.T) {
	args := ffmpegSource{url: "/tmp/clip.mp4", fps: 1}.captureArgs()
	if slices.Contains(args, "-protocol_whitelist") || slices.Contains(args, "-rtsp_transport") {
		t.Fatalf("a file gets RTSP options: %q", args)
	}
}
