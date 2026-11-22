package main

import "time"

// With gcode_park the slicer parks the print head out of the camera's view
// for a few seconds right after every layer change (see docs/gcode-park.md).
// The frame taken while the head stands still there is the one to keep: in
// the first park_window of a layer it is the frame that differs least from
// the frames before and after it. Frames in that window are also spared when
// a long layer is thinned out, so the park is not lost.

// setPark turns the park selection on for this session.
func (s *session) setPark(on bool, window time.Duration) {
	s.park, s.parkWindow = on, window
}

// inParkWindow reports whether a frame taken at at belongs to the start of
// the layer that began with the frame at start.
func (s *session) inParkWindow(start, at time.Time) bool {
	return s.park && at.Sub(start) < s.parkWindow
}

// pickParked answers the index of the stillest frame among the frames taken
// in the first window of the layer, or -1 when there are fewer than three.
func pickParked(frames []buffered, window time.Duration) int {
	if len(frames) < 3 {
		return -1
	}

	end := 0
	for end < len(frames) && frames[end].at.Sub(frames[0].at) < window {
		end++
	}

	if end < 3 {
		return -1
	}

	best, bestScore := -1, int64(-1)

	for i := 1; i < end-1; i++ {
		score := difference(frames[i-1].thumb, frames[i].thumb) + difference(frames[i].thumb, frames[i+1].thumb)
		if best < 0 || score < bestScore {
			best, bestScore = i, score
		}
	}

	return best
}

// difference is the sum of the brightness differences of two thumbnails.
func difference(a, b []uint8) int64 {
	var sum int64

	for i := range min(len(a), len(b)) {
		d := int64(a[i]) - int64(b[i])
		if d < 0 {
			d = -d
		}

		sum += d
	}

	return sum
}
