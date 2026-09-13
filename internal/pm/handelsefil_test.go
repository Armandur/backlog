package pm

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// Webben delar filhändelsens text på verbet för att hitta sökvägen. Skulle ett
// verb bara finnas på ena sidan skulle filen visas utan länk, utan att något
// går sönder synligt. Provet jämför därför listorna mot varandra.
func TestWebbenKannerSammaFilverbSomPM(t *testing.T) {
	kalla, err := os.ReadFile("../pmweb/static/pm-samtal.js")
	if err != nil {
		t.Fatalf("kunde inte läsa pm-samtal.js: %v", err)
	}
	rad := regexp.MustCompile(`const FILVERB = \[(.*?)\];`).FindSubmatch(kalla)
	if rad == nil {
		t.Fatal("hittade ingen FILVERB-lista i pm-samtal.js")
	}
	webben := map[string]bool{}
	for _, del := range strings.Split(string(rad[1]), ",") {
		if verb := strings.Trim(strings.TrimSpace(del), `"`); verb != "" {
			webben[verb] = true
		}
	}
	if len(webben) != len(Filverb) {
		t.Fatalf("webben känner %d verb, PM skriver %d: %v mot %v", len(webben), len(Filverb), webben, Filverb)
	}
	for _, verb := range Filverb {
		if !webben[verb] {
			t.Errorf("verbet %q saknas i pm-samtal.js", verb)
		}
	}
}

// Verben i koden ska vara samma som i listan. Ett nytt verb i en switch utan
// rad i Filverb är just det fel provet ovan inte kan se.
func TestFilverbTackerVerbenIKoden(t *testing.T) {
	kalla, err := os.ReadFile("handelse.go")
	if err != nil {
		t.Fatalf("kunde inte läsa handelse.go: %v", err)
	}
	kant := map[string]bool{}
	for _, verb := range Filverb {
		kant[verb] = true
	}
	for _, traff := range regexp.MustCompile(`verb (?:=|:=) "([^"]+)"`).FindAllSubmatch(kalla, -1) {
		if !kant[string(traff[1])] {
			t.Errorf("verbet %q står i handelse.go men saknas i Filverb", traff[1])
		}
	}
}
