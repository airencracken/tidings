package reader

import (
	"archive/zip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"time"
)

// Smaller account downloads keep the original JSON format. Larger downloads
// contain numbered JSON files that the existing importer can merge in order.
func (a *App) downloadReadingData(w http.ResponseWriter, r *http.Request) {
	data, err := a.Store.Export(r.Context(), current(r).User)
	if err != nil {
		a.fail(w, r, err)
		return
	}

	if len(data.Stories) <= maxRestoredStories {
		// Stop measuring at the import limit without allocating one large JSON
		// document for the common case of many saved full-text stories.
		docs := 0
		var first []byte
		err := exportDocuments(data, func(document []byte) error {
			docs++
			first = document
			if docs > 1 {
				return errMultipleDocuments
			}
			return nil
		})
		if err == nil {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Content-Disposition", `attachment; filename="tidings.json"`)
			w.Header().Set("Content-Length", strconv.Itoa(len(first)))
			if r.Method != http.MethodHead {
				_, _ = w.Write(first)
			}
			return
		}
		if err != errMultipleDocuments {
			a.fail(w, r, err)
			return
		}
	}
	a.downloadReadingArchive(w, r, data)
}

func (a *App) downloadReadingArchive(w http.ResponseWriter, r *http.Request, data Export) {
	file, err := os.CreateTemp("", "tidings-reading-*.zip")
	if err != nil {
		a.fail(w, r, err)
		return
	}
	defer os.Remove(file.Name())
	defer file.Close()
	if err := writeReadingArchive(file, data); err != nil {
		a.fail(w, r, err)
		return
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		a.fail(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="tidings-reading.zip"`)
	http.ServeContent(w, r, "", time.Time{}, file)
}

func writeReadingArchive(dst io.Writer, data Export) error {
	archive := zip.NewWriter(dst)
	part := 0
	if err := exportDocuments(data, func(document []byte) error {
		part++
		entry, err := archive.Create(fmt.Sprintf("tidings-%06d.json", part))
		if err != nil {
			return err
		}
		_, err = entry.Write(document)
		return err
	}); err != nil {
		return err
	}
	return archive.Close()
}

// exportDocuments preserves the subscription, group and story formats that
// older importers understand. Each file has the sources needed by its stories.
func exportDocuments(data Export, write func([]byte) error) error {
	stories := data.Stories
	data.Stories = []Entry{}
	return splitReadingExport(data, stories, write)
}

var errMultipleDocuments = errors.New("reading data requires multiple documents")

type readingDocument struct {
	Export
	Stories []json.RawMessage `json:"stories"`
}

// splitReadingExport counts the exact encoded size including punctuation.
// The site limits total sources and groups, so only stories need partitioning.
func splitReadingExport(header Export, stories []Entry, write func([]byte) error) error {
	doc := readingDocument{Export: header, Stories: []json.RawMessage{}}
	base, err := json.Marshal(doc)
	if err != nil {
		return err
	}
	if len(base) > MaxRestoreBytes {
		return errors.New("reading data contains too much source or group metadata")
	}
	size := len(base)
	flush := func() error {
		body, err := json.Marshal(doc)
		if err != nil {
			return err
		}
		return write(body)
	}
	for _, story := range stories {
		data, err := json.Marshal(story)
		if err != nil {
			return err
		}
		if len(base)+len(data) > MaxRestoreBytes {
			return errors.New("a story is too large for a reading-data file")
		}
		if len(doc.Stories) > 0 && (size+len(data)+1 > MaxRestoreBytes || len(doc.Stories) == maxRestoredStories) {
			if err := flush(); err != nil {
				return err
			}
			doc.Stories = []json.RawMessage{}
			size = len(base)
		}
		if len(doc.Stories) > 0 {
			size++
		}
		doc.Stories = append(doc.Stories, data)
		size += len(data)
	}
	return flush()
}
