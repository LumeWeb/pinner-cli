package internal

import (
	"fmt"
	"io"

	"github.com/ipfs/go-cid"
	"github.com/ipld/go-car/v2"
)

var cidUndefSlice = []cid.Cid{cid.Undef}

func GetCarRoots(reader io.Reader, inspect bool) ([]cid.Cid, error) {
	readerAt, ok := reader.(io.ReaderAt)
	if !ok {
		return cidUndefSlice, fmt.Errorf("reader does not implement io.ReaderAt")
	}
	carReader, err := car.NewReader(readerAt)
	if err != nil {
		return cidUndefSlice, err
	}
	defer carReader.Close()

	if inspect {
		_, err = carReader.Inspect(true)
		if err != nil {
			return cidUndefSlice, err
		}

	}

	roots, err := carReader.Roots()
	if err != nil {
		return cidUndefSlice, err
	}
	if len(roots) == 0 {
		return cidUndefSlice, fmt.Errorf("no roots found in CAR file")
	}

	// Reset reader position if it's seekable, so caller can read full CAR content
	if seeker, ok := reader.(io.Seeker); ok {
		_, err = seeker.Seek(0, io.SeekStart)
		if err != nil {
			return nil, fmt.Errorf("failed to reset reader position: %w", err)
		}
	}

	return roots, nil
}
