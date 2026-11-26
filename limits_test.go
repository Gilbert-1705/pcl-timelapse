package main

import (
	"bytes"
	"errors"
	"image"
	"image/jpeg"
	"testing"
)

func encodedJPEG(t *testing.T, w, h int) []byte {
	t.Helper()

	var out bytes.Buffer
	if err := jpeg.Encode(&out, image.NewGray(image.Rect(0, 0, w, h)), &jpeg.Options{Quality: 10}); err != nil {
		t.Fatal(err)
	}

	return out.Bytes()
}

// SEC-0006: a picture that claims huge dimensions is refused from its header,
// before it is decoded, both for the frame choice and for the preview.
func TestAHugeCameraPictureIsNotDecoded(t *testing.T) {
	huge := encodedJPEG(t, 8000, 8000)

	if _, err := decodeJPEG(huge); !errors.Is(err, errPictureTooLarge) {
		t.Fatalf("an 8000 × 8000 picture: %v", err)
	}

	if _, err := thumbnail(huge); !errors.Is(err, errPictureTooLarge) {
		t.Fatalf("the frame choice took an 8000 × 8000 picture: %v", err)
	}

	if scaledJPEG(huge, 960) != nil {
		t.Fatal("the preview took an 8000 × 8000 picture")
	}

	for _, size := range [][2]int{{1920, 1080}, {3840, 2160}, {4096, 4096}} {
		if _, err := decodeJPEG(encodedJPEG(t, size[0], size[1])); err != nil {
			t.Fatalf("%d × %d: %v", size[0], size[1], err)
		}
	}
}

// SEC-0014: the job id names a folder; only plain ids do.
func TestOnlyAPlainJobIDNamesAFolder(t *testing.T) {
	for _, id := range []string{"0193a8e2-7c4f-7b10-9d2e-5f6a7b8c9d0e", "J1", "7", "job_42"} {
		if !validJobID(id) {
			t.Errorf("%q is refused", id)
		}
	}

	for _, id := range []string{"", "..", "../x", "a/b", `a\b`, "a.b", "a b", "ä", string(make([]byte, 65))} {
		if validJobID(id) {
			t.Errorf("%q is taken", id)
		}
	}
}
