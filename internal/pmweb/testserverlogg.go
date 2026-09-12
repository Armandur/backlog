package pmweb

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/mazen160/backlog/internal/pm"
)

const (
	testserverLoggTail       = 200
	testserverLoggMaxTail    = 10_000
	testserverLoggMaxLasning = 1 << 20
)

func (s *Server) strommaTestserverlogg(w http.ResponseWriter, r *http.Request) {
	alias := r.PathValue("alias")
	if _, err := pm.NewSamtalStore(s.db).ProjectIDByAlias(r.Context(), alias); err != nil {
		svaraFel(w, err, http.StatusNotFound)
		return
	}
	tail, err := testserverTail(r)
	if err != nil {
		svaraFel(w, err, http.StatusBadRequest)
		return
	}

	store := pm.NewTestserverStore(s.db, pm.Konfig{}, konfigWorkDir())
	server, err := store.Hamta(r.Context(), alias)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		svaraFel(w, fmt.Errorf("kunde inte läsa testservern: %w", err), http.StatusInternalServerError)
		return
	}
	logg := filepath.Join(konfigWorkDir(), "loggar", "testserver-"+alias+".log")
	if server != nil && server.Logg != "" {
		logg = server.Logg
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		svaraFel(w, errors.New("servern kan inte strömma testserverns logg"), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	fil, oppningsfel := os.Open(logg)
	if oppningsfel != nil && !errors.Is(oppningsfel, os.ErrNotExist) {
		_ = skrivTestserverLoggslut(w, flusher, "Testserverns logg kunde inte läsas.")
		return
	}
	if fil != nil {
		defer fil.Close()
		start, sokfel := hittaTailStart(fil, tail)
		if sokfel != nil {
			_ = skrivTestserverLoggslut(w, flusher, "Testserverns logg kunde inte läsas.")
			return
		}
		if _, sokfel = fil.Seek(start, io.SeekStart); sokfel != nil {
			_ = skrivTestserverLoggslut(w, flusher, "Testserverns logg kunde inte läsas.")
			return
		}
	}

	lever := server != nil && server.Lever
	if fil == nil {
		if err := skrivTestserverLoggrad(w, flusher, "Testserverns logg finns inte ännu."); err != nil {
			return
		}
		if !lever {
			_ = skrivTestserverLoggslut(w, flusher, "Testservern kör inte.")
			return
		}
	}

	var ofullstandig []byte
	for {
		if fil == nil {
			if oppnad, err := os.Open(logg); err == nil {
				fil = oppnad
				defer fil.Close()
			}
		}
		if fil != nil {
			ofullstandig, err = lasLoggrader(fil, ofullstandig, func(rad []byte) error {
				return skrivTestserverLoggrad(w, flusher, string(rad))
			})
			if err != nil {
				return
			}
		}

		server, err = store.Hamta(r.Context(), alias)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return
		}
		if errors.Is(err, sql.ErrNoRows) || !server.Lever {
			if len(ofullstandig) > 0 {
				if err := skrivTestserverLoggrad(w, flusher, string(ofullstandig)); err != nil {
					return
				}
			}
			_ = skrivTestserverLoggslut(w, flusher, "Testservern har stoppats.")
			return
		}

		timer := time.NewTimer(stromIntervall)
		select {
		case <-r.Context().Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

func testserverTail(r *http.Request) (int, error) {
	varde := r.URL.Query().Get("tail")
	if varde == "" {
		return testserverLoggTail, nil
	}
	tail, err := strconv.Atoi(varde)
	if err != nil || tail < 0 || tail > testserverLoggMaxTail {
		return 0, fmt.Errorf("tail måste vara ett heltal mellan 0 och %d", testserverLoggMaxTail)
	}
	return tail, nil
}

func hittaTailStart(fil *os.File, tail int) (int64, error) {
	info, err := fil.Stat()
	if err != nil {
		return 0, err
	}
	storlek := info.Size()
	if tail == 0 {
		return storlek, nil
	}
	minsta := storlek - testserverLoggMaxLasning
	if minsta < 0 {
		minsta = 0
	}
	position := storlek
	antalRadslut := 0
	hoppaSista := true
	buffert := make([]byte, 32*1024)
	for position > minsta {
		blockstart := position - int64(len(buffert))
		if blockstart < minsta {
			blockstart = minsta
		}
		block := buffert[:position-blockstart]
		if _, err := fil.ReadAt(block, blockstart); err != nil && !errors.Is(err, io.EOF) {
			return 0, err
		}
		for i := len(block) - 1; i >= 0; i-- {
			if block[i] != '\n' {
				hoppaSista = false
				continue
			}
			if hoppaSista {
				hoppaSista = false
				continue
			}
			antalRadslut++
			if antalRadslut == tail {
				return blockstart + int64(i) + 1, nil
			}
		}
		position = blockstart
	}
	return minsta, nil
}

func lasLoggrader(fil *os.File, rest []byte, skriv func([]byte) error) ([]byte, error) {
	buffert := make([]byte, 32*1024)
	for {
		antal, err := fil.Read(buffert)
		if antal > 0 {
			rest = append(rest, buffert[:antal]...)
			for {
				slut := bytes.IndexByte(rest, '\n')
				if slut < 0 {
					break
				}
				rad := bytes.TrimSuffix(rest[:slut], []byte{'\r'})
				if err := skriv(rad); err != nil {
					return rest, err
				}
				rest = rest[slut+1:]
			}
		}
		if errors.Is(err, io.EOF) {
			return rest, nil
		}
		if err != nil {
			return rest, err
		}
	}
}

func skrivTestserverLoggrad(w io.Writer, flusher http.Flusher, rad string) error {
	data, err := json.Marshal(rad)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "data: %s\n\n", data); err != nil {
		return err
	}
	flusher.Flush()
	return nil
}

func skrivTestserverLoggslut(w io.Writer, flusher http.Flusher, besked string) error {
	data, err := json.Marshal(besked)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "event: slut\ndata: %s\n\n", data); err != nil {
		return err
	}
	flusher.Flush()
	return nil
}
