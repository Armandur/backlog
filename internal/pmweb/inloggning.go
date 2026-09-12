package pmweb

import (
	"crypto/subtle"
	"fmt"
	"net/http"
	"os"

	"github.com/mazen160/backlog/internal/pm"
)

const (
	// StandardAnvandare och StandardLosenord gäller bara tills någon skriver
	// ett eget [webb]-block. PM varnar vid start så länge de används.
	StandardAnvandare = "admin"
	StandardLosenord  = "admin"
	// LosenordMiljo vinner över konfigurationsfilen.
	LosenordMiljo = "BACKLOG_PM_LOSENORD"
)

// Inloggning är namnet och lösenordet som PM-webben kräver.
type Inloggning struct {
	Anvandare string
	Losenord  string
	// Standard är sant när varken filen eller miljön sade något.
	Standard bool
}

// InloggningUrKonfig väljer inloggning: miljövariabeln först, sedan
// [webb] i pm.toml, annars admin och admin.
func InloggningUrKonfig(konfig pm.Konfig) Inloggning {
	logg := Inloggning{Anvandare: konfig.Webb.Anvandare, Losenord: konfig.Webb.Losenord}
	if franMiljo := os.Getenv(LosenordMiljo); franMiljo != "" {
		logg.Losenord = franMiljo
	}
	if logg.Anvandare == "" {
		logg.Anvandare = StandardAnvandare
	}
	if logg.Losenord == "" {
		logg.Losenord = StandardLosenord
		logg.Standard = true
	}
	return logg
}

// Varning beskriver att PM kör med standardlösenordet, eller är tom.
func (l Inloggning) Varning(konfigfil string) string {
	if !l.Standard {
		return ""
	}
	return fmt.Sprintf("PM kör med lösenordet %s, som alla känner till. Skriv ett eget under [webb] i %s, eller sätt %s.\n",
		StandardLosenord, konfigfil, LosenordMiljo)
}

// KravInloggning lägger inloggningen framför alla rutter, även upstreams.
func KravInloggning(nasta http.Handler, logg Inloggning) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		anvandare, losenord, angivet := r.BasicAuth()
		if angivet && stammer(anvandare, logg.Anvandare) && stammer(losenord, logg.Losenord) {
			nasta.ServeHTTP(w, r)
			return
		}
		w.Header().Set("WWW-Authenticate", `Basic realm="backlog-pm", charset="UTF-8"`)
		svaraFel(w, fmt.Errorf("logga in för att använda PM"), http.StatusUnauthorized)
	})
}

// stammer jämför utan att läcka hur långt PM hann jämföra.
func stammer(givet, vantat string) bool {
	return subtle.ConstantTimeCompare([]byte(givet), []byte(vantat)) == 1
}
