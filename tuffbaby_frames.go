package tuiffects

import (
	"bytes"
	"compress/zlib"
	"encoding/base64"
	"io"
	"sync"
)

// The tuffbaby reference frames. See the header of effect_tuffbaby.go for
// where they come from and what may and may not be claimed about them; the
// short version is that this effect is original to this package and the
// likeness is not, being a heavy reduction of a GIF published under Tenor's
// "tuff baby" tag.
//
// The source is 55 frames at 10fps, 498 by 498, greyscale. Each frame is
// reduced to one tone per terminal cell: '.' is not part of the picture and
// '1' to '5' run darkest to brightest, matching the source's own tonality
// rather than inverting it. The reduction is a straight luminance quantise
// with a temporal median of three across neighbouring frames, because
// quantising video noise into five bands makes single cells flicker between
// them, and a despeckle pass over each frame.
//
// The grid is already in terminal proportions: the source is square and a
// cell is about twice as tall as it is wide, so 72 by 36 is the square
// picture, not a squashed one.
const (
	tuffMaskWidth  = 72
	tuffMaskHeight = 36
	tuffFrameCount = 55
)

// tuffFrameData is the whole sequence, deflated and base64'd: 142560 cells of
// plain text compress to 17976 bytes, which is worth the twelve lines of
// decoder below to keep out of the source tree.
const tuffFrameData = "" +
	"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" +
	"Cz4884AjPrzbKAql/wOF8nal"

// tuffFrames returns the decoded sequence, frame major, each frame being
// tuffMaskHeight rows of tuffMaskWidth tone characters.
//
// It decodes once. A caller that never runs this effect never pays for it,
// which is the reason it is not an init.
func tuffFrames() [][]string {
	tuffFramesOnce.Do(func() {
		raw, err := base64.StdEncoding.DecodeString(tuffFrameData)
		if err != nil {
			panic("tuiffects: tuffbaby frame data is not valid base64: " + err.Error())
		}
		reader, err := zlib.NewReader(bytes.NewReader(raw))
		if err != nil {
			panic("tuiffects: tuffbaby frame data is not valid zlib: " + err.Error())
		}
		defer reader.Close()
		flat, err := io.ReadAll(reader)
		if err != nil {
			panic("tuiffects: tuffbaby frame data is truncated: " + err.Error())
		}
		want := tuffMaskWidth * tuffMaskHeight * tuffFrameCount
		if len(flat) != want {
			panic("tuiffects: tuffbaby frame data is the wrong size")
		}
		tuffDecoded = make([][]string, tuffFrameCount)
		text := string(flat)
		for frame := range tuffDecoded {
			grid := make([]string, tuffMaskHeight)
			for row := range grid {
				start := (frame*tuffMaskHeight + row) * tuffMaskWidth
				grid[row] = text[start : start+tuffMaskWidth]
			}
			tuffDecoded[frame] = grid
		}
	})
	return tuffDecoded
}

var (
	tuffFramesOnce sync.Once
	tuffDecoded    [][]string
)
