package pm

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/mazen160/backlog/internal/timeutil"
)

// KvotKonfig styr när PM byter agent för att kvoten tagit slut. Utan
// turordning är funktionen av, och regelmotorn bestämmer ensam.
type KvotKonfig struct {
	// Tak är andelen förbrukad kvot där PM börjar leta efter en annan agent.
	Tak float64 `toml:"tak" json:"tak"`
	// Turordning listar agenterna som får ta varandras arbete. Bara agenter
	// som gör samma sorts jobb hör hemma här, PM kan inte avgöra det själv.
	Turordning []string `toml:"turordning" json:"turordning"`
}

// StandardKvottak gäller när konfigurationen inte säger något annat.
const StandardKvottak = 0.9

// KvotFarskhet är hur gammal en avläsning får vara och ändå styra agentvalet.
// Förbrukningen växer mellan avläsningarna, så en gammal siffra är alltid för
// låg. Har agenten inte kört på ett dygn vet PM för lite för att flytta arbete.
const KvotFarskhet = 24 * time.Hour

// fonsterlangd är hur lång period fönstret mäter. Den behövs för att avgöra om
// avläsningen gjordes i det fönster som pågår nu.
var fonsterlangd = map[string]time.Duration{"fem": 5 * time.Hour, "sju": 7 * 24 * time.Hour}

// Anstrangning är den hårdast belastade kvoten agenten har, som andel.
//
// Två sorters gammal uppgift sorteras bort. Ett fönster vars nollställning
// passerat räknas inte, för kvoten har börjat om. Och en avläsning som gjordes
// före det pågående fönstret började räknas inte heller, för då gäller siffran
// ett fönster som redan är slut.
func (a Anvandning) Anstrangning() (float64, bool) {
	hogst, hittad := 0.0, false
	nu := timeutil.Now()
	fonster := map[string]*Kvotfonster{"fem": a.FemTimmar, "sju": a.SjuDagar}
	for namn, f := range fonster {
		if f == nil || f.NollstallsAt <= nu {
			continue
		}
		if a.AvlastAt > 0 && a.AvlastAt < f.NollstallsAt-int64(fonsterlangd[namn]) {
			continue
		}
		if !hittad || f.Andel > hogst {
			hogst, hittad = f.Andel, true
		}
	}
	return hogst, hittad
}

// Farsk säger om avläsningen är ny nog att styra ett agentbyte.
func (a Anvandning) Farsk() bool {
	return a.AvlastAt > 0 && timeutil.Now()-a.AvlastAt <= int64(KvotFarskhet)
}

// ValjAgentEfterKvot byter agent när den valda har passerat kvottaket och
// någon annan i turordningen har utrymme. Överstyrning rör den aldrig:
// säger användaren --agent gäller det, full kvot eller inte.
func ValjAgentEfterKvot(k Konfig, val AgentVal, overstyrd bool, lagen map[string]Anvandning) AgentVal {
	if overstyrd || len(k.Kvot.Turordning) < 2 {
		return val
	}
	if !innehaller(k.Kvot.Turordning, val.Agent) {
		return val
	}
	tak := k.Kvot.Tak
	if tak <= 0 || tak > 1 {
		tak = StandardKvottak
	}
	// En gammal avläsning får inte flytta arbete. Siffran kan vara långt
	// under det verkliga läget, åt båda hållen.
	if !lagen[val.Agent].Farsk() {
		return val
	}
	anstrangning, kant := lagen[val.Agent].Anstrangning()
	if !kant || anstrangning < tak {
		return val
	}

	kandidater := ledigaAgenter(k, val.Agent, tak, lagen)
	if len(kandidater) == 0 {
		val.Motivering += fmt.Sprintf(", kvoten är på %d procent men ingen annan agent har utrymme",
			procent(anstrangning))
		return val
	}
	vald := kandidater[0]
	val.Agent = vald.namn
	val.Motivering += fmt.Sprintf(", bytte till %s eftersom kvoten låg på %d procent mot %d",
		vald.namn, procent(anstrangning), procent(vald.anstrangning))
	// Regelns modell och ansträngning hörde till den gamla agenten.
	val.Modell, val.Anstrangning = "", ""
	return val
}

type kvotkandidat struct {
	namn         string
	anstrangning float64
}

func ledigaAgenter(k Konfig, undantag string, tak float64, lagen map[string]Anvandning) []kvotkandidat {
	var kandidater []kvotkandidat
	for _, namn := range k.Kvot.Turordning {
		if namn == undantag {
			continue
		}
		if _, finns := k.Agenter[namn]; !finns {
			continue
		}
		if !lagen[namn].Farsk() {
			// En gammal avläsning säger inget om läget nu. Agenten kan ha
			// arbetat hårt sedan dess.
			continue
		}
		anstrangning, kant := lagen[namn].Anstrangning()
		if !kant {
			// En agent PM aldrig läst av kan lika gärna vara full. Den får
			// stå över, annars blir bytet en gissning.
			continue
		}
		if anstrangning >= tak {
			continue
		}
		kandidater = append(kandidater, kvotkandidat{namn: namn, anstrangning: anstrangning})
	}
	sort.Slice(kandidater, func(i, j int) bool {
		if kandidater[i].anstrangning != kandidater[j].anstrangning {
			return kandidater[i].anstrangning < kandidater[j].anstrangning
		}
		return kandidater[i].namn < kandidater[j].namn
	})
	return kandidater
}

func procent(andel float64) int { return int(andel*100 + 0.5) }

// LasKvotlagen hämtar kvotläget för agenterna i turordningen. Ett fel ger en
// tom karta, och då byter PM ingen agent.
func LasKvotlagen(ctx context.Context, db *sql.DB, k Konfig) map[string]Anvandning {
	lagen := make(map[string]Anvandning, len(k.Kvot.Turordning))
	store := NewAnvandningStore(db)
	for _, namn := range k.Kvot.Turordning {
		if strings.TrimSpace(namn) == "" {
			continue
		}
		lage, err := store.Hamta(ctx, namn)
		if err != nil || lage.Saknas {
			continue
		}
		lagen[namn] = lage
	}
	return lagen
}
