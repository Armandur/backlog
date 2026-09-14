package pm

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/mazen160/backlog/internal/timeutil"
)

// Handelse beskriver ett steg i en pågående agentkörning.
type Handelse struct {
	Tid  int64  `json:"tid"`
	Sort string `json:"sort"`
	Text string `json:"text"`
}

type handelseSkrivare struct {
	sokvag  string
	mu      sync.Mutex
	fil     *os.File
	buffert *bufio.Writer
	kodare  *json.Encoder
	fel     error
	stangd  bool
}

// nyHandelseSkrivare skapar filen först när en händelse faktiskt skrivs. En
// agent utan strömning ska inte lämna en tom fil efter sig.
func nyHandelseSkrivare(logg string) (*handelseSkrivare, error) {
	return &handelseSkrivare{sokvag: handelseSokvag(logg)}, nil
}

func (s *handelseSkrivare) oppna() error {
	if s.fil != nil {
		return nil
	}
	fil, err := os.Create(s.sokvag)
	if err != nil {
		return err
	}
	s.fil = fil
	s.buffert = bufio.NewWriter(fil)
	s.kodare = json.NewEncoder(s.buffert)
	return nil
}

func handelseSokvag(logg string) string {
	andelse := filepath.Ext(logg)
	return strings.TrimSuffix(logg, andelse) + ".handelser.jsonl"
}

// HandelseSokvag ger sökvägen till händelsefilen bredvid en körningslogg.
func HandelseSokvag(logg string) string { return handelseSokvag(logg) }

func (s *handelseSkrivare) Skriv(handelse Handelse) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.fel != nil || s.stangd {
		return
	}
	if err := s.oppna(); err != nil {
		s.fel = err
		return
	}
	if err := s.kodare.Encode(handelse); err != nil {
		s.fel = err
		return
	}
	if err := s.buffert.Flush(); err != nil {
		s.fel = err
	}
}

func (s *handelseSkrivare) Stang() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stangd {
		return s.fel
	}
	s.stangd = true
	if s.fil == nil {
		return s.fel
	}
	if err := s.buffert.Flush(); err != nil && s.fel == nil {
		s.fel = err
	}
	if err := s.fil.Close(); err != nil && s.fel == nil {
		s.fel = err
	}
	return s.fel
}

func nyHandelse(sort, text string) Handelse {
	return Handelse{Tid: timeutil.Now(), Sort: sort, Text: kortaHandelsetext(text)}
}

func kortaHandelsetext(text string) string {
	text = strings.Join(strings.Fields(text), " ")
	tecken := []rune(text)
	if len(tecken) <= 200 {
		return text
	}
	langd := 200
	for {
		dolda := len(tecken) - langd
		slut := fmt.Sprintf("... %d tecken till", dolda)
		nyLangd := 200 - len([]rune(slut))
		if nyLangd < 0 {
			nyLangd = 0
		}
		if nyLangd == langd {
			return string(tecken[:langd]) + slut
		}
		langd = nyLangd
	}
}

// SkrivAvbrottshandelse lägger en sista rad i körningens händelsefil när
// städningen hittar en körning vars process är borta. Då ser även den som
// ansluter långt efteråt varför körningen tog slut.
func SkrivAvbrottshandelse(logg string) {
	if logg == "" {
		return
	}
	fil, err := os.OpenFile(HandelseSokvag(logg), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer fil.Close()
	data, err := json.Marshal(nyHandelse("fel", "körningen avbröts, processen finns inte längre"))
	if err != nil {
		return
	}
	_, _ = fil.Write(append(data, '\n'))
}
