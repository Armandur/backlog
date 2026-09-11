package pm

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/mazen160/backlog/internal/timeutil"
)

const reservationstid = 15 * time.Minute

var errPortReserverad = errors.New("PM har redan reserverat porten")

// PortIntervall anger första och sista porten som PM får välja.
type PortIntervall struct {
	Fran int `toml:"fran" json:"fran"`
	Till int `toml:"till" json:"till"`
}

// Portreservation är en port som PM har tilldelat ett projekt.
type Portreservation struct {
	Port         int    `json:"port"`
	Projekt      string `json:"projekt"`
	PID          int    `json:"pid"`
	ReserveradAt int64  `json:"reserverad_at"`
}

// PortStore läser och skriver portreservationer.
type PortStore struct{ db *sql.DB }

func NewPortStore(db *sql.DB) *PortStore { return &PortStore{db: db} }

// LedigPort hittar den första porten som operativsystemet låter PM binda.
func LedigPort(intervall PortIntervall) (int, error) {
	if err := intervall.validera(); err != nil {
		return 0, err
	}
	for port := intervall.Fran; port <= intervall.Till; port++ {
		ledig, err := provaPort(port)
		if err != nil {
			return 0, err
		}
		if ledig {
			return port, nil
		}
	}
	return 0, fmt.Errorf("ingen ledig port finns mellan %d och %d", intervall.Fran, intervall.Till)
}

// Reservera väljer och reserverar en port. fastPort vinner när den är större än noll.
func (s *PortStore) Reservera(ctx context.Context, projekt string, pid int, intervall PortIntervall, fastPort int) (*Portreservation, error) {
	projekt = strings.TrimSpace(projekt)
	if projekt == "" {
		return nil, fmt.Errorf("ange vilket projekt som ska få porten")
	}
	if pid <= 0 {
		return nil, fmt.Errorf("processens pid måste vara större än noll")
	}
	if err := intervall.validera(); err != nil {
		return nil, err
	}
	if fastPort > 0 {
		return s.reserveraFast(ctx, projekt, pid, intervall, fastPort)
	}
	return s.reserveraFranIntervall(ctx, projekt, pid, intervall)
}

// Skapa sparar reservationen. PM får ersätta en förfallen reservation.
func (s *PortStore) Skapa(ctx context.Context, reservation *Portreservation) error {
	reservation.ReserveradAt = timeutil.Now()
	resultat, err := s.db.ExecContext(ctx, `
		INSERT INTO pm_portar(port, projekt, pid, reserverad_at) VALUES(?,?,?,?)
		ON CONFLICT(port) DO UPDATE SET
			projekt=excluded.projekt,
			pid=excluded.pid,
			reserverad_at=excluded.reserverad_at
		WHERE pm_portar.reserverad_at <= ?`,
		reservation.Port, reservation.Projekt, reservation.PID, reservation.ReserveradAt,
		reservation.ReserveradAt-int64(reservationstid))
	if err != nil {
		return fmt.Errorf("reservera port %d: %w", reservation.Port, err)
	}
	antal, err := resultat.RowsAffected()
	if err != nil {
		return fmt.Errorf("kontrollera reservationen för port %d: %w", reservation.Port, err)
	}
	if antal == 0 {
		return errPortReserverad
	}
	return nil
}

func (s *PortStore) reserveraFranIntervall(ctx context.Context, projekt string, pid int, intervall PortIntervall) (*Portreservation, error) {
	for port := intervall.Fran; port <= intervall.Till; port++ {
		ledig, err := provaPort(port)
		if err != nil {
			return nil, err
		}
		if !ledig {
			continue
		}
		reservation := &Portreservation{Port: port, Projekt: projekt, PID: pid}
		if err := s.Skapa(ctx, reservation); errors.Is(err, errPortReserverad) {
			continue
		} else if err != nil {
			return nil, err
		}
		return reservation, nil
	}
	return nil, fmt.Errorf("ingen ledig port finns mellan %d och %d", intervall.Fran, intervall.Till)
}

func (s *PortStore) reserveraFast(ctx context.Context, projekt string, pid int, intervall PortIntervall, port int) (*Portreservation, error) {
	if port < 1 || port > 65535 {
		return nil, fmt.Errorf("den fasta porten %d måste vara mellan 1 och 65535", port)
	}
	ledig, err := provaPort(port)
	if err != nil {
		return nil, err
	}
	if ledig {
		reservation := &Portreservation{Port: port, Projekt: projekt, PID: pid}
		err = s.Skapa(ctx, reservation)
		if err == nil {
			return reservation, nil
		}
		if !errors.Is(err, errPortReserverad) {
			return nil, err
		}
	}
	forslag, forslagErr := s.forslag(ctx, intervall)
	if forslagErr == nil {
		return nil, fmt.Errorf("port %d är upptagen, prova den lediga porten %d", port, forslag)
	}
	return nil, fmt.Errorf("port %d är upptagen och ingen ledig port finns mellan %d och %d", port, intervall.Fran, intervall.Till)
}

func (s *PortStore) forslag(ctx context.Context, intervall PortIntervall) (int, error) {
	for port := intervall.Fran; port <= intervall.Till; port++ {
		reservationer, err := s.fraga(ctx, `SELECT `+portkolumner+` FROM pm_portar WHERE port=? AND reserverad_at>?`,
			port, timeutil.Now()-int64(reservationstid))
		if err != nil {
			return 0, err
		}
		if len(reservationer) > 0 {
			continue
		}
		ledig, err := provaPort(port)
		if err != nil {
			return 0, err
		}
		if ledig {
			return port, nil
		}
	}
	return 0, fmt.Errorf("ingen ledig port finns")
}

const portkolumner = `port, projekt, pid, reserverad_at`

func (s *PortStore) fraga(ctx context.Context, q string, args ...any) ([]Portreservation, error) {
	rader, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("läs portreservationer: %w", err)
	}
	defer rader.Close()
	reservationer := []Portreservation{}
	for rader.Next() {
		var reservation Portreservation
		if err := rader.Scan(&reservation.Port, &reservation.Projekt, &reservation.PID, &reservation.ReserveradAt); err != nil {
			return nil, err
		}
		reservationer = append(reservationer, reservation)
	}
	return reservationer, rader.Err()
}

func (intervall PortIntervall) validera() error {
	if intervall.Fran == 0 && intervall.Till == 0 {
		return fmt.Errorf("portintervallet saknas")
	}
	if intervall.Fran < 1 || intervall.Till > 65535 || intervall.Fran > intervall.Till {
		return fmt.Errorf("portintervallet måste gå från en lägre till en högre port mellan 1 och 65535")
	}
	return nil
}

func provaPort(port int) (bool, error) {
	lyssnare, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return false, nil
	}
	if err := lyssnare.Close(); err != nil {
		return false, fmt.Errorf("kunde inte stänga kontrollen av port %d: %w", port, err)
	}
	return true, nil
}
