package pm

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Hälsokontrollen har egen cache- och timeoutlogik, som kan ändras utan
// att påverka ägarskapet och signaleringen av testserverprocesser.
type testserverHalsoNyckel struct {
	db           *sql.DB
	alias, halsa string
	pid, port    int
}

type testserverHalsoSvar struct {
	klar       chan struct{}
	uppe       bool
	giltigTill time.Time
}

var testserverHalsocache = struct {
	sync.Mutex
	svar map[testserverHalsoNyckel]*testserverHalsoSvar
}{svar: make(map[testserverHalsoNyckel]*testserverHalsoSvar)}

// Status kontrollerar både processen och om dess HTTP-port svarar.
func (s *TestserverStore) Status(ctx context.Context, alias string) (*Testserver, error) {
	server, err := s.Hamta(ctx, alias)
	if errors.Is(err, sql.ErrNoRows) {
		return &Testserver{Alias: alias, Status: TestserverNere}, nil
	}
	if err != nil {
		return nil, err
	}
	if !server.Lever {
		server.Status = TestserverKrasch
		server.Exitkod = lasTestserverExitkod(server.Logg, server.PID)
		return server, nil
	}
	halsa := s.konfig.Testserver[alias].Halsa
	if halsa == "" {
		halsa = "/"
	}
	if s.halsaSvarar(ctx, server, halsa) {
		server.Status = TestserverUppe
	} else {
		server.Status = TestserverStartar
	}
	return server, nil
}

func (s *TestserverStore) halsaSvarar(ctx context.Context, server *Testserver, halsa string) bool {
	if !strings.HasPrefix(halsa, "/") {
		halsa = "/" + halsa
	}
	nyckel := testserverHalsoNyckel{
		db: s.db, alias: server.Alias, pid: server.PID, port: server.Port, halsa: halsa,
	}
	for {
		nu := time.Now()
		testserverHalsocache.Lock()
		for gammalNyckel, gammaltSvar := range testserverHalsocache.svar {
			select {
			case <-gammaltSvar.klar:
				if !nu.Before(gammaltSvar.giltigTill) {
					delete(testserverHalsocache.svar, gammalNyckel)
				}
			default:
			}
		}
		befintligt := testserverHalsocache.svar[nyckel]
		if befintligt != nil {
			select {
			case <-befintligt.klar:
				if nu.Before(befintligt.giltigTill) {
					uppe := befintligt.uppe
					testserverHalsocache.Unlock()
					return uppe
				}
				delete(testserverHalsocache.svar, nyckel)
			default:
				klar := befintligt.klar
				testserverHalsocache.Unlock()
				select {
				case <-ctx.Done():
					return false
				case <-klar:
					continue
				}
			}
		}
		svar := &testserverHalsoSvar{klar: make(chan struct{})}
		testserverHalsocache.svar[nyckel] = svar
		testserverHalsocache.Unlock()

		klient := &http.Client{
			Timeout: s.halsotid,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return http.ErrUseLastResponse
			},
		}
		adress := fmt.Sprintf("http://localhost:%d%s", server.Port, halsa)
		begaran, err := http.NewRequestWithContext(ctx, http.MethodGet, adress, nil)
		uppe := false
		if err == nil {
			if httpSvar, anropsfel := klient.Do(begaran); anropsfel == nil {
				uppe = true
				httpSvar.Body.Close()
			}
		}

		testserverHalsocache.Lock()
		svar.uppe = uppe
		svar.giltigTill = time.Now().Add(s.cachetid)
		close(svar.klar)
		testserverHalsocache.Unlock()
		return uppe
	}
}
