package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/prusa3d/prusa-connect/sdk/storageapi"
)

// The diagnosis keeps every candidate of some layers, so the choice can be
// checked against what the camera saw. diagnose_layers says how many layers
// of a print, diagnose_every which ones: every n-th layer from the n-th on.
// The frames wait in the session folder and go into the print's folder
// beside the video, under diagnose/, scaled to diagnoseWidth.

const diagnoseWidth = 960

func (s *session) setDiagnosis(layers, every int) {
	s.diagnoseLayers, s.diagnoseEvery = layers, max(1, every)
}

func (s *session) diagnosing() bool {
	return s.diagnoseLayers > 0 && s.meta.Diagnosed < s.diagnoseLayers &&
		s.meta.Layers%s.diagnoseEvery == 0
}

type candidateNote struct {
	File     string  `json:"datei"`
	At       string  `json:"zeit"`
	Distance int64   `json:"abweichung"`
	Relative float64 `json:"abweichung_relativ"`
	Chosen   bool    `json:"gewaehlt,omitempty"`
}

type layerNote struct {
	Layer      int             `json:"schicht"`
	Chosen     int             `json:"gewaehlt"`
	Candidates []candidateNote `json:"kandidaten"`
	History    int             `json:"verlauf_bilder"`
	Weight     int             `json:"verlauf_gewicht"`
	Note       string          `json:"hinweis"`
}

// keepCandidates copies the frames of the closing layer into
// diagnose/schicht-NNNN in the session folder, with the reference picture
// and how far each frame was from it.
func (s *session) keepCandidates(closing []buffered, verdict judgement, history int) error {
	dir := filepath.Join(s.dir, "diagnose", fmt.Sprintf("schicht-%04d", s.meta.Layers))
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}

	note := layerNote{
		Layer: s.meta.Layers, Chosen: verdict.best + 1, History: history, Weight: verdict.weight,
		Note: "abweichung: Summe der Helligkeitsunterschiede zum Referenzbild (160x90 Graustufen), " +
			"kleiner ist freier. referenz.png ist das Referenzbild.",
	}

	least := int64(-1)
	for _, v := range verdict.scores {
		if least < 0 || v < least {
			least = v
		}
	}

	for i, b := range closing {
		file := fmt.Sprintf("kandidat-%03d.jpg", i+1)
		if err := copyFile(b.file, filepath.Join(dir, file)); err != nil {
			return err
		}

		c := candidateNote{File: file, At: b.at.Format(time.RFC3339Nano), Chosen: i == verdict.best}
		if i < len(verdict.scores) {
			c.Distance = verdict.scores[i]
			if least > 0 {
				c.Relative = float64(verdict.scores[i]) / float64(least)
			}
		}

		note.Candidates = append(note.Candidates, c)
	}

	if len(verdict.reference) == thumbWidth*thumbHeight {
		img := image.NewGray(image.Rect(0, 0, thumbWidth, thumbHeight))
		for p, v := range verdict.reference {
			img.SetGray(p%thumbWidth, p/thumbWidth, color.Gray{Y: v})
		}

		var out bytes.Buffer
		if err := png.Encode(&out, img); err == nil {
			_ = os.WriteFile(filepath.Join(dir, "referenz.png"), out.Bytes(), 0o640)
		}
	}

	raw, _ := json.MarshalIndent(note, "", "  ")
	if err := os.WriteFile(filepath.Join(dir, "auswahl.json"), raw, 0o640); err != nil {
		return err
	}

	s.meta.Diagnosed++

	return nil
}

// storeDiagnosis writes what keepCandidates kept into folder/diagnose.
func storeDiagnosis(ctx context.Context, files *storageapi.Client, storage, folder, sessionDir string) error {
	root := filepath.Join(sessionDir, "diagnose")

	var paths []string
	_ = filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			paths = append(paths, p)
		}

		return nil
	})
	sort.Strings(paths)

	for _, p := range paths {
		rel, err := filepath.Rel(root, p)
		if err != nil {
			continue
		}

		target := folder + "/diagnose/" + filepath.ToSlash(rel)

		if strings.HasSuffix(p, ".jpg") {
			picture, err := os.ReadFile(p)
			if err != nil {
				return err
			}

			if small := scaledJPEG(picture, diagnoseWidth); len(small) > 0 {
				picture = small
			}

			if err = writeBytes(ctx, files, storage, target, picture); err != nil {
				return err
			}

			continue
		}

		if err := writeFile(ctx, files, storage, target, p); err != nil {
			return err
		}
	}

	return nil
}

func copyFile(from, to string) error {
	raw, err := os.ReadFile(from)
	if err != nil {
		return err
	}

	return os.WriteFile(to, raw, 0o640)
}
