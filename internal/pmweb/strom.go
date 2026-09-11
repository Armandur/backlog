package pmweb

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/mazen160/backlog/internal/pm"
)

const stromIntervall = 300 * time.Millisecond

func (s *Server) strommaKorning(w http.ResponseWriter, r *http.Request) {
	store := pm.NewKorningStore(s.db)
	korning, err := store.Hamta(r.Context(), r.PathValue("id"))
	if err != nil {
		svaraFel(w, err, http.StatusNotFound)
		return
	}

	logg := korning.Logg
	if logg == "" {
		logg = filepath.Join(pm.LoggKatalog(konfigWorkDir()), korning.ID+".log")
	}
	sokvag := pm.HandelseSokvag(logg)
	fil, err := os.Open(sokvag)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		svaraFel(w, fmt.Errorf("kunde inte läsa körningens händelser: %w", err), http.StatusInternalServerError)
		return
	}
	if fil != nil {
		defer func() { fil.Close() }()
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		svaraFel(w, errors.New("servern kan inte strömma körningen"), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	// En körning vars process är borta ska säga det, även om den aldrig hann
	// skriva en händelse. Städningen skriver då raden åt oss.
	if uppdaterad, stadad, stadfel := store.StadaOmOvergiven(r.Context(), korning.ID); stadfel == nil {
		korning = uppdaterad
		if stadad && fil == nil {
			if oppnad, oppningsfel := os.Open(sokvag); oppningsfel == nil {
				fil = oppnad
			}
		}
	}

	// Filen skapas först när agenten skickar sin första händelse. En körning
	// som just startat har alltså ingen fil än, och då väntar vi in den.
	if fil == nil && avslutad(korning.Status) {
		_ = skrivStromHandelse(w, flusher, "slut", pm.Handelse{
			Tid: time.Now().UnixNano(), Sort: "text", Text: "körningen strömmar inte",
		})
		return
	}

	var ofullstandig []byte
	avbruten := false
	for {
		if fil == nil {
			if oppnad, oppningsfel := os.Open(sokvag); oppningsfel == nil {
				fil = oppnad
			}
		}
		if fil != nil {
			var skrivfel error
			ofullstandig, skrivfel = lasHandelser(fil, ofullstandig, func(rad []byte) error {
				return skrivRadata(w, flusher, rad)
			})
			if skrivfel != nil {
				return
			}
		}

		var stadad bool
		korning, stadad, err = store.StadaOmOvergiven(r.Context(), korning.ID)
		if err != nil {
			return
		}
		// Bara städningen vet att processen var borta. Ett vanligt fel från
		// agenten ska behålla sitt eget besked.
		avbruten = avbruten || stadad
		if avslutad(korning.Status) && len(ofullstandig) == 0 {
			if fil == nil {
				// Körningen hann bli klar utan att skicka en enda händelse.
				_ = skrivStromHandelse(w, flusher, "slut", pm.Handelse{
					Tid: time.Now().UnixNano(), Sort: "text", Text: "körningen strömmar inte",
				})
				return
			}
			_ = skrivStromHandelse(w, flusher, "slut", slutHandelse(korning, avbruten))
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

func lasHandelser(fil *os.File, rest []byte, skriv func([]byte) error) ([]byte, error) {
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
				rad := bytes.TrimSpace(rest[:slut])
				if len(rad) > 0 {
					if err := skriv(rad); err != nil {
						return rest, err
					}
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

func skrivRadata(w io.Writer, flusher http.Flusher, data []byte) error {
	if !json.Valid(data) {
		return errors.New("händelsefilen innehåller ogiltig JSON")
	}
	if _, err := fmt.Fprintf(w, "data: %s\n\n", data); err != nil {
		return err
	}
	flusher.Flush()
	return nil
}

func skrivStromHandelse(w io.Writer, flusher http.Flusher, namn string, handelse pm.Handelse) error {
	data, err := json.Marshal(handelse)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", namn, data); err != nil {
		return err
	}
	flusher.Flush()
	return nil
}

func avslutad(status string) bool { return status == pm.StatusKlar || status == pm.StatusFel }

func slutHandelse(korning *pm.Korning, avbruten bool) pm.Handelse {
	tid := time.Now().UnixNano()
	if korning.SlutAt != nil {
		tid = *korning.SlutAt
	}
	exitkod := "saknas"
	if korning.ExitKod != nil {
		exitkod = fmt.Sprint(*korning.ExitKod)
	}
	sort := "text"
	text := "körningen är klar"
	if korning.Status == pm.StatusFel {
		sort = "fel"
		text = "körningen avslutades med fel"
	}
	if avbruten {
		sort = "fel"
		text = "körningen avbröts, processen finns inte längre"
	}
	return pm.Handelse{Tid: tid, Sort: sort, Text: fmt.Sprintf("%s (exitkod %s)", text, exitkod)}
}
