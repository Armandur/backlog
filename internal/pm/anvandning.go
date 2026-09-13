package pm

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/mazen160/backlog/internal/timeutil"
)

// Kvotfonster beskriver hur stor andel av ett tidsfönster agenten har använt.
type Kvotfonster struct {
	Andel        float64 `json:"andel"`
	NollstallsAt int64   `json:"nollstalls_at"`
}

// Anvandning är PM:s senaste kända kvotläge för en agent.
type Anvandning struct {
	Agent string `json:"agent"`
	// Saknas betyder att PM inte läst av något ännu. Rapporterar säger om
	// agenten alls kan leverera ett kvotläge, se KvotstromFinns.
	Saknas      bool         `json:"saknas"`
	Rapporterar bool         `json:"rapporterar"`
	Status      string       `json:"status,omitempty"`
	Kvottyp     string       `json:"kvottyp,omitempty"`
	FemTimmar   *Kvotfonster `json:"fem_timmar,omitempty"`
	SjuDagar    *Kvotfonster `json:"sju_dagar,omitempty"`
	AvlastAt    int64        `json:"avlast_at,omitempty"`
}

// KvotstromFinns säger om PM alls kan få ett kvotläge från agenten. Claude bär
// det i sin ström. Codex ström bär tokens men ingen kvot, så den frågas i
// stället via app-servern, se CodexKvot. Övriga agenter har ingen kvot.
func KvotstromFinns(strom string) bool {
	return strom == "claude-json" || strom == "codex-json"
}

type AnvandningStore struct{ db *sql.DB }

func NewAnvandningStore(db *sql.DB) *AnvandningStore { return &AnvandningStore{db: db} }

func (s *AnvandningStore) Spara(ctx context.Context, lage Anvandning) error {
	if strings.TrimSpace(lage.Agent) == "" {
		return fmt.Errorf("kvotläget saknar agent")
	}
	// Codex kvot bär inte alltid båda fönstren. Ett läge utan något fönster
	// alls säger däremot ingenting, och sparas inte.
	if lage.FemTimmar == nil && lage.SjuDagar == nil {
		return fmt.Errorf("kvotläget saknar tidsfönster")
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO pm_anvandning(
		agent, status, kvottyp, fem_timmar_andel, fem_timmar_nollstalls_at,
		sju_dagar_andel, sju_dagar_nollstalls_at, avlast_at
	) VALUES(?,?,?,?,?,?,?,?)
	ON CONFLICT(agent) DO UPDATE SET
		status=excluded.status, kvottyp=excluded.kvottyp,
		fem_timmar_andel=excluded.fem_timmar_andel,
		fem_timmar_nollstalls_at=excluded.fem_timmar_nollstalls_at,
		sju_dagar_andel=excluded.sju_dagar_andel,
		sju_dagar_nollstalls_at=excluded.sju_dagar_nollstalls_at,
		avlast_at=excluded.avlast_at
	WHERE excluded.avlast_at >= pm_anvandning.avlast_at`,
		lage.Agent, lage.Status, lage.Kvottyp,
		fonsterandel(lage.FemTimmar), fonstertid(lage.FemTimmar),
		fonsterandel(lage.SjuDagar), fonstertid(lage.SjuDagar), lage.AvlastAt)
	if err != nil {
		return fmt.Errorf("spara kvotläge för %s: %w", lage.Agent, err)
	}
	return nil
}

// Ett fönster som agenten inte rapporterar skrivs som null, inte som noll.
// En nolla hade sett ut som en oanvänd kvot.
func fonsterandel(fonster *Kvotfonster) any {
	if fonster == nil {
		return nil
	}
	return fonster.Andel
}

func fonstertid(fonster *Kvotfonster) any {
	if fonster == nil {
		return nil
	}
	return fonster.NollstallsAt
}

func (s *AnvandningStore) Hamta(ctx context.Context, agent string) (Anvandning, error) {
	lage := Anvandning{Agent: agent}
	var femAndel, sjuAndel sql.NullFloat64
	var femTid, sjuTid sql.NullInt64
	err := s.db.QueryRowContext(ctx, `SELECT status, kvottyp,
		fem_timmar_andel, fem_timmar_nollstalls_at,
		sju_dagar_andel, sju_dagar_nollstalls_at, avlast_at
		FROM pm_anvandning WHERE agent=?`, agent).Scan(
		&lage.Status, &lage.Kvottyp, &femAndel, &femTid,
		&sjuAndel, &sjuTid, &lage.AvlastAt)
	if err == sql.ErrNoRows {
		lage.Saknas = true
		return lage, nil
	}
	if err != nil {
		return Anvandning{}, fmt.Errorf("läs kvotläge för %s: %w", agent, err)
	}
	if femAndel.Valid {
		lage.FemTimmar = &Kvotfonster{Andel: femAndel.Float64, NollstallsAt: femTid.Int64}
	}
	if sjuAndel.Valid {
		lage.SjuDagar = &Kvotfonster{Andel: sjuAndel.Float64, NollstallsAt: sjuTid.Int64}
	}
	return lage, nil
}

func tolkaClaudeAnvandning(rad []byte) (Anvandning, error) {
	var post struct {
		Type          string `json:"type"`
		RateLimitInfo struct {
			Status         string `json:"status"`
			RateLimitType  string `json:"rateLimitType"`
			UnifiedWindows struct {
				FiveHour struct {
					Utilization float64 `json:"utilization"`
					ResetsAt    int64   `json:"resetsAt"`
				} `json:"five_hour"`
				SevenDay struct {
					Utilization float64 `json:"utilization"`
					ResetsAt    int64   `json:"resetsAt"`
				} `json:"seven_day"`
			} `json:"unifiedWindows"`
		} `json:"rate_limit_info"`
	}
	if err := json.Unmarshal(rad, &post); err != nil {
		return Anvandning{}, err
	}
	if post.Type != "rate_limit_event" {
		return Anvandning{}, fmt.Errorf("raden är inget kvotläge")
	}
	fem := post.RateLimitInfo.UnifiedWindows.FiveHour
	sju := post.RateLimitInfo.UnifiedWindows.SevenDay
	if fem.Utilization < 0 || fem.Utilization > 1 || sju.Utilization < 0 || sju.Utilization > 1 {
		return Anvandning{}, fmt.Errorf("kvotandelen ligger utanför intervallet 0 till 1")
	}
	if fem.ResetsAt <= 0 || sju.ResetsAt <= 0 {
		return Anvandning{}, fmt.Errorf("kvotläget saknar nollställningstid")
	}
	// Claude räknar i sekunder, PM i nanosekunder. Blandade enheter i samma
	// svar blir fel i vyn, så tiderna räknas om här.
	return Anvandning{
		Status: post.RateLimitInfo.Status, Kvottyp: post.RateLimitInfo.RateLimitType,
		FemTimmar: &Kvotfonster{Andel: fem.Utilization, NollstallsAt: fem.ResetsAt * int64(time.Second)},
		SjuDagar:  &Kvotfonster{Andel: sju.Utilization, NollstallsAt: sju.ResetsAt * int64(time.Second)},
		AvlastAt:  timeutil.Now(),
	}, nil
}

// TokensUrClaudeStrom ger körningens totala tokens. Resultatraden bär hela
// körningens summa, och den vinner över de enskilda stegen.
func TokensUrClaudeStrom(utdata string) int {
	summa, total := 0, 0
	for _, rad := range strings.Split(utdata, "\n") {
		rad = strings.TrimSpace(rad)
		if rad == "" || !strings.Contains(rad, "usage") {
			continue
		}
		var post claudeRad
		if json.Unmarshal([]byte(rad), &post) != nil {
			continue
		}
		tokens := claudeNyaTokens(post.Usage, post.Message)
		if tokens == 0 {
			continue
		}
		if post.Type == "result" {
			total = tokens
			continue
		}
		if post.Type == "assistant" {
			summa += tokens
		}
	}
	if total > 0 {
		return total
	}
	return summa
}

// AnvandningUrClaudeStrom plockar det sista kvotläget ur en claude-ström.
// Den läser samlad utdata, precis som ModellUrClaudeStrom, så ingen del av
// koden behöver gissa var databasen ligger.
func AnvandningUrClaudeStrom(utdata string) *Anvandning {
	var senaste *Anvandning
	for _, rad := range strings.Split(utdata, "\n") {
		rad = strings.TrimSpace(rad)
		if rad == "" || !strings.Contains(rad, "rate_limit_event") {
			continue
		}
		lage, err := tolkaClaudeAnvandning([]byte(rad))
		if err != nil {
			continue
		}
		kopia := lage
		senaste = &kopia
	}
	return senaste
}

// SparaKvotlage lägger en körnings kvotläge på agenten. Alla körningssorter
// går genom den, så en fråga eller ett förslag håller siffran lika färsk som
// en utdelad task. Ett fel här får aldrig stoppa körningen som gått bra.
func SparaKvotlage(ctx context.Context, db *sql.DB, agent string, lage *Anvandning) {
	if lage == nil {
		return
	}
	kopia := *lage
	kopia.Agent = agent
	if err := NewAnvandningStore(db).Spara(ctx, kopia); err != nil {
		fmt.Fprintf(os.Stderr, "kunde inte spara kvotläget för %s: %v\n", agent, err)
	}
}
