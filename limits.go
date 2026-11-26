package main

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"regexp"
)

// maxSide bounds a camera picture before it is decoded. A JPEG of a few
// megabytes can claim 20000 × 20000 pixels and take more memory to decode
// than the module may use (needs.memory_mib), so only its header is read
// first. 4096 covers every camera up to 4K.
const maxSide = 4096

var errPictureTooLarge = errors.New("the camera picture is larger than 4096 × 4096 pixels")

// decodeJPEG decodes a camera picture after checking its size in the header.
func decodeJPEG(picture []byte) (image.Image, error) {
	config, err := jpeg.DecodeConfig(bytes.NewReader(picture))
	if err != nil {
		return nil, err
	}

	if config.Width > maxSide || config.Height > maxSide {
		return nil, fmt.Errorf("%w: %d × %d", errPictureTooLarge, config.Width, config.Height)
	}

	return jpeg.Decode(bytes.NewReader(picture))
}

// jobIDPattern is what a job id may look like before it names a folder in the
// data directory. The host hands out UUIDs; anything with a slash, a dot or
// other characters is not recorded.
var jobIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

func validJobID(id string) bool { return jobIDPattern.MatchString(id) }
