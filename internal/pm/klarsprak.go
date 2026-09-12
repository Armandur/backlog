package pm

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
)

// KlarsprakResultat beskriver linterpoängen och de fynd agenten kan rätta.
type KlarsprakResultat struct {
	Ord                int            `json:"ord"`
	Totalt             int            `json:"totalt"`
	TotaltPer100Ord    float64        `json:"totalt_per100o"`
	Overtradelser      map[string]int `json:"overtradelser"`
	ExempelForbjudna   []string       `json:"exempel_forbjudna"`
	ExempelMarknadsord []string       `json:"exempel_marknadsord"`
}

// LintaKlarsprak kör den valfria lintern. Ett avslaget eller tomt val hoppas över.
func LintaKlarsprak(ctx context.Context, konfig SystemKonfig, text string) (*KlarsprakResultat, error) {
	if !konfig.KlarsprakAktiv || strings.TrimSpace(konfig.KlarsprakSokvag) == "" {
		return nil, nil
	}
	if _, err := os.Stat(konfig.KlarsprakSokvag); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("kunde inte kontrollera klarspråkslintern: %w", err)
	}

	tmp, err := os.CreateTemp("", "backlog-pm-klarsprak-*.txt")
	if err != nil {
		return nil, fmt.Errorf("kunde inte skapa linterfilen: %w", err)
	}
	namn := tmp.Name()
	defer os.Remove(namn)
	defer tmp.Close()
	if _, err := tmp.WriteString(text); err != nil {
		return nil, fmt.Errorf("kunde inte skriva linterfilen: %w", err)
	}
	if _, err := tmp.Seek(0, 0); err != nil {
		return nil, fmt.Errorf("kunde inte läsa linterfilen: %w", err)
	}

	kommando := exec.CommandContext(ctx, "python3", konfig.KlarsprakSokvag)
	kommando.Stdin = tmp
	utdata, err := kommando.Output()
	if err != nil {
		return nil, fmt.Errorf("klarspråkslintern misslyckades: %w", err)
	}
	var resultat KlarsprakResultat
	if err := json.Unmarshal(utdata, &resultat); err != nil {
		return nil, fmt.Errorf("klarspråkslintern gav ett ogiltigt svar: %w", err)
	}
	return &resultat, nil
}

// Fyndtext ger agenten ett kort och stabilt underlag för omskrivningen.
func (r KlarsprakResultat) Fyndtext() string {
	namn := make([]string, 0, len(r.Overtradelser))
	for kategori, antal := range r.Overtradelser {
		if antal > 0 {
			namn = append(namn, fmt.Sprintf("%s: %d", kategori, antal))
		}
	}
	sort.Strings(namn)
	delar := []string{fmt.Sprintf("Poäng: %.2f fynd per hundra ord.", r.TotaltPer100Ord)}
	if len(namn) > 0 {
		delar = append(delar, "Kategorier: "+strings.Join(namn, ", ")+".")
	}
	if len(r.ExempelForbjudna) > 0 {
		delar = append(delar, "Förbjudna uttryck: "+strings.Join(r.ExempelForbjudna, ", ")+".")
	}
	if len(r.ExempelMarknadsord) > 0 {
		delar = append(delar, "Marknadsord: "+strings.Join(r.ExempelMarknadsord, ", ")+".")
	}
	return strings.Join(delar, "\n")
}

// GranskaAgenttext lintar svaret och ber agenten skriva om texten högst en gång.
func GranskaAgenttext(
	ctx context.Context,
	konfig SystemKonfig,
	agent Agent,
	svar string,
	text string,
) (string, *KlarsprakResultat, bool) {
	resultat, err := LintaKlarsprak(ctx, konfig, text)
	if err != nil || resultat == nil || resultat.TotaltPer100Ord <= konfig.KlarsprakMaxPer100 {
		return svar, resultat, false
	}

	prompt := fmt.Sprintf(`Skriv om ditt JSON-svar på svenska med linterns fynd som underlag.
Behåll exakt samma JSON-fält och samma sakuppgifter.
Svara endast med JSON-objektet utan kodstaket eller förklaringar.

Linterns fynd:
%s

Ditt tidigare svar:
%s`, resultat.Fyndtext(), svar)
	omskrivet, err := agent.Fraga(ctx, prompt)
	if err != nil {
		return svar, resultat, false
	}
	omskrivet = strings.TrimSpace(omskrivet)
	omskrivenText, err := agenttext(omskrivet)
	if err != nil {
		return svar, resultat, false
	}
	andraResultatet, err := LintaKlarsprak(ctx, konfig, omskrivenText)
	if err != nil {
		return omskrivet, nil, true
	}
	return omskrivet, andraResultatet, true
}

func agenttext(svar string) (string, error) {
	var falt struct {
		Titel       string `json:"titel"`
		Beskrivning string `json:"beskrivning"`
	}
	if err := json.Unmarshal([]byte(svar), &falt); err != nil {
		return "", err
	}
	if strings.TrimSpace(falt.Titel) == "" || strings.TrimSpace(falt.Beskrivning) == "" {
		return "", fmt.Errorf("agentsvaret saknar titel eller beskrivning")
	}
	return falt.Titel + "\n\n" + falt.Beskrivning, nil
}
