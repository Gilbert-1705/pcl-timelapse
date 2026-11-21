package main

import (
	"bytes"
	"image"
	"image/jpeg"
)

// Thumbnails are small greyscale copies of each frame. They are what the
// selection compares; the full JPEG stays on disk.
const (
	thumbWidth  = 160
	thumbHeight = 90
)

// thumbnail decodes a JPEG and scales it to a thumbWidth×thumbHeight greyscale
// image by averaging the source pixels that fall into each cell.
func thumbnail(frame []byte) ([]uint8, error) {
	img, err := jpeg.Decode(bytes.NewReader(frame))
	if err != nil {
		return nil, err
	}

	return shrink(img), nil
}

func shrink(img image.Image) []uint8 {
	bounds := img.Bounds()
	w, h := bounds.Dx(), bounds.Dy()

	sums := make([]uint32, thumbWidth*thumbHeight)
	counts := make([]uint32, thumbWidth*thumbHeight)

	// A YCbCr image, which is what a camera JPEG decodes to, carries the
	// luma plane directly. Anything else goes through the colour model.
	if ycc, ok := img.(*image.YCbCr); ok {
		for y := range h {
			ty := y * thumbHeight / h
			row := ycc.Y[y*ycc.YStride : y*ycc.YStride+w]

			for x, v := range row {
				cell := ty*thumbWidth + x*thumbWidth/w
				sums[cell] += uint32(v)
				counts[cell]++
			}
		}
	} else {
		for y := range h {
			ty := y * thumbHeight / h

			for x := range w {
				r, g, b, _ := img.At(bounds.Min.X+x, bounds.Min.Y+y).RGBA()
				luma := (19595*r + 38470*g + 7471*b + 1<<15) >> 24
				cell := ty*thumbWidth + x*thumbWidth/w
				sums[cell] += luma
				counts[cell]++
			}
		}
	}

	out := make([]uint8, thumbWidth*thumbHeight)
	for i := range out {
		if counts[i] > 0 {
			out[i] = uint8(sums[i] / counts[i])
		}
	}

	return out
}

// historyLength is how many of the last kept frames make up the reference.
const historyLength = 8

// pickClearest answers the index of the frame on which the print head hides
// the least.
//
// Every frame is compared with a reference picture of the scene without the
// head, and the closest one wins. The reference is the per-pixel median of a
// pool of frames: the frames of this layer and the frames kept for the last
// few layers. Kept frames are the clearest of their layer, and the head was
// somewhere else on each of them, so their median is close to an empty view
// of the print, even when the head spends most of this layer above the
// object. The kept frames therefore weigh twice as much as this layer's
// frames together. The object grows by one layer at a time, which changes
// every candidate alike and does not decide the choice.
//
// Before there is any history, the reference is the median of this layer
// alone, which works while the head does not stay in one place for more than
// half the layer.
//
// Each frame is compared after its mean brightness is matched to the
// reference's, so a camera adjusting its exposure does not decide the choice.
func pickClearest(thumbs, history [][]uint8) int {
	return judge(thumbs, history).best
}

// judgement is how pickClearest decided: the winner, every frame's distance
// to the reference and the reference itself. The diagnosis writes it out.
type judgement struct {
	best      int
	scores    []int64
	reference []uint8
	weight    int
}

func judge(thumbs, history [][]uint8) judgement {
	switch len(thumbs) {
	case 0:
		return judgement{best: -1}
	case 1:
		return judgement{best: 0, scores: []int64{0}}
	}

	if len(history) < 3 && len(thumbs) < 3 {
		// Two frames and nothing to judge them by; take the later one.
		return judgement{best: len(thumbs) - 1, scores: make([]int64, len(thumbs))}
	}

	weight := 0
	if len(history) >= 3 {
		weight = max(1, (2*len(thumbs)+len(history)-1)/len(history))
	}

	reference := weightedMedian(thumbs, history, weight)
	referenceMean := mean(reference)
	best, bestScore := -1, int64(-1)
	scores := make([]int64, len(thumbs))

	for i, t := range thumbs {
		shift := referenceMean - mean(t)

		var score int64

		for p, v := range t {
			d := int64(v) + shift - int64(reference[p])
			if d < 0 {
				d = -d
			}

			score += d
		}

		scores[i] = score

		// Ties go to the later frame: it shows the layer more complete.
		if best < 0 || score <= bestScore {
			best, bestScore = i, score
		}
	}

	return judgement{best: best, scores: scores, reference: reference, weight: weight}
}

// weightedMedian is the per-pixel median of frames, each counted once, and
// history, each counted weight times. It counts values into a histogram, so
// the cost is the number of frames plus 256 per pixel.
func weightedMedian(frames, history [][]uint8, weight int) []uint8 {
	size := len(frames[0])
	out := make([]uint8, size)
	total := len(frames) + weight*len(history)
	half := total / 2

	var hist [256]int

	for p := range size {
		hist = [256]int{}

		for _, f := range frames {
			hist[f[p]]++
		}

		if weight > 0 {
			for _, h := range history {
				hist[h[p]] += weight
			}
		}

		seen := 0

		for v, n := range hist {
			seen += n
			if seen > half {
				out[p] = uint8(v)

				break
			}
		}
	}

	return out
}

func mean(values []uint8) int64 {
	if len(values) == 0 {
		return 0
	}

	var sum int64
	for _, v := range values {
		sum += int64(v)
	}

	return sum / int64(len(values))
}
