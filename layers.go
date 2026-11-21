package main

import "time"

// zTolerance is how far two Z readings may differ and still be one layer.
// Layer heights start at 0.05 mm, so 0.02 mm keeps every real change apart
// from rounding in the telemetry.
const zTolerance = 0.02

// layerTracker turns the printer's Z telemetry into layer boundaries.
//
// The printer reports Z every few seconds, and only when it changed. A reading
// can catch the nozzle in a Z hop on a travel move, which looks like a new
// layer that is gone again a few seconds later. So a rise is only a candidate:
// it becomes a layer change once no lower reading has come back for `settle`.
// The boundary is placed at the moment of the first rise, not at the moment it
// was confirmed, so frames are split at the right time.
type layerTracker struct {
	settle time.Duration

	known   bool
	floor   float64 // Z of the layer being printed
	started time.Time

	pending   bool
	pendingZ  float64
	pendingAt time.Time
}

func newLayerTracker(settle time.Duration) *layerTracker {
	return &layerTracker{settle: settle}
}

// reset starts a print: z is the height the printer reports right now, or
// NaN-free zero when it reports none.
func (l *layerTracker) reset(at time.Time, z float64, known bool) {
	*l = layerTracker{settle: l.settle, known: known, floor: z, started: at}
}

// observe takes one Z reading.
func (l *layerTracker) observe(at time.Time, z float64) {
	if !l.known {
		l.known, l.floor, l.started = true, z, at

		return
	}

	switch {
	case z > l.floor+zTolerance:
		if !l.pending {
			l.pending, l.pendingAt = true, at
		}
		// A hop seen after a real change raises the candidate, and the return
		// from the hop lowers it again; the moment of the first rise stays.
		l.pendingZ = z
	case z < l.floor-zTolerance:
		// Lower than the layer: the floor itself was a hop that got confirmed,
		// or the print moved down. Take the new height without a boundary.
		l.pending = false
		l.floor = z
	default:
		// Back on the layer: the rise was a hop.
		l.pending = false
	}
}

// tick confirms a pending change once it has held for settle, and answers the
// boundary it confirmed.
func (l *layerTracker) tick(now time.Time) (time.Time, bool) {
	if !l.pending || now.Sub(l.pendingAt) < l.settle {
		return time.Time{}, false
	}

	boundary := l.pendingAt
	l.floor, l.started, l.pending = l.pendingZ, boundary, false

	return boundary, true
}

// height is the Z of the layer being printed.
func (l *layerTracker) height() float64 { return l.floor }
