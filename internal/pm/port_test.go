package pm

import (
	"context"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/mazen160/backlog/internal/timeutil"
)

func lyssnaPaLedigPort(t *testing.T) (net.Listener, int) {
	t.Helper()
	lyssnare, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatalf("öppna testport: %v", err)
	}
	t.Cleanup(func() {
		if err := lyssnare.Close(); err != nil && !strings.Contains(err.Error(), "closed") {
			t.Errorf("stäng testport: %v", err)
		}
	})
	return lyssnare, lyssnare.Addr().(*net.TCPAddr).Port
}

func hamtaLedigPort(t *testing.T) int {
	t.Helper()
	lyssnare, port := lyssnaPaLedigPort(t)
	if err := lyssnare.Close(); err != nil {
		t.Fatalf("frigör testport: %v", err)
	}
	return port
}

func intervallEfter(port int) PortIntervall {
	if port <= 65525 {
		return PortIntervall{Fran: port, Till: port + 10}
	}
	return PortIntervall{Fran: port - 10, Till: port}
}

func TestLedigPortHopparOverUpptagenPort(t *testing.T) {
	_, upptagen := lyssnaPaLedigPort(t)
	port, err := LedigPort(intervallEfter(upptagen))
	if err != nil {
		t.Fatal(err)
	}
	if port == upptagen {
		t.Fatalf("PM valde den upptagna porten %d", upptagen)
	}
}

func TestReserveraSpararOchHopparOverAktivReservation(t *testing.T) {
	db := testDB(t)
	store := NewPortStore(db)
	ctx := context.Background()
	reserverad := hamtaLedigPort(t)
	if err := store.Skapa(ctx, &Portreservation{Port: reserverad, Projekt: "forsta", PID: 11}); err != nil {
		t.Fatal(err)
	}

	reservation, err := store.Reservera(ctx, "andra", 22, intervallEfter(reserverad), 0)
	if err != nil {
		t.Fatal(err)
	}
	if reservation.Port == reserverad {
		t.Fatalf("PM återanvände den aktiva reservationen på port %d", reserverad)
	}
	var projekt string
	var pid int
	var reserveradAt int64
	if err := db.QueryRow(`SELECT projekt, pid, reserverad_at FROM pm_portar WHERE port=?`, reservation.Port).
		Scan(&projekt, &pid, &reserveradAt); err != nil {
		t.Fatal(err)
	}
	if projekt != "andra" || pid != 22 || reserveradAt == 0 {
		t.Fatalf("PM sparade fel reservation: projekt=%q pid=%d tid=%d", projekt, pid, reserveradAt)
	}
}

func TestForfallenReservationTasIgen(t *testing.T) {
	db := testDB(t)
	store := NewPortStore(db)
	port := hamtaLedigPort(t)
	gammal := timeutil.Now() - int64(16*time.Minute)
	if _, err := db.Exec(`INSERT INTO pm_portar(port, projekt, pid, reserverad_at) VALUES(?,?,?,?)`,
		port, "gammalt", 11, gammal); err != nil {
		t.Fatal(err)
	}

	reservation, err := store.Reservera(context.Background(), "nytt", 22, PortIntervall{Fran: port, Till: port}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if reservation.Port != port || reservation.Projekt != "nytt" || reservation.PID != 22 {
		t.Fatalf("PM ersatte inte den förfallna reservationen: %+v", reservation)
	}
}

func TestFastLedigPortVinner(t *testing.T) {
	db := testDB(t)
	store := NewPortStore(db)
	fastPort := hamtaLedigPort(t)
	annanPort := hamtaLedigPort(t)

	reservation, err := store.Reservera(context.Background(), "demo", 33,
		PortIntervall{Fran: annanPort, Till: annanPort}, fastPort)
	if err != nil {
		t.Fatal(err)
	}
	if reservation.Port != fastPort {
		t.Fatalf("PM valde port %d i stället för den fasta porten %d", reservation.Port, fastPort)
	}
}

func TestFastUpptagenPortGerLedigtForslag(t *testing.T) {
	db := testDB(t)
	store := NewPortStore(db)
	_, upptagen := lyssnaPaLedigPort(t)
	forslag := hamtaLedigPort(t)

	_, err := store.Reservera(context.Background(), "demo", 33,
		PortIntervall{Fran: forslag, Till: forslag}, upptagen)
	if err == nil {
		t.Fatal("PM godtog den upptagna fasta porten")
	}
	if !strings.Contains(err.Error(), "upptagen") || !strings.Contains(err.Error(), fmt.Sprint(forslag)) {
		t.Fatalf("felet saknar ett ledigt förslag: %v", err)
	}
}

func TestTomtOchOgiltigtPortintervallGerFel(t *testing.T) {
	for _, intervall := range []PortIntervall{{}, {Fran: 8200, Till: 8100}, {Fran: -1, Till: 10}} {
		if _, err := LedigPort(intervall); err == nil {
			t.Fatalf("intervallet %+v godtogs", intervall)
		}
	}
}
