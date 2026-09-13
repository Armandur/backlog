package pmweb

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Skriptfilerna laddas efter varandra i samma globala scope. Två filer som
// deklarerar samma namn skuggar tyst varandra, och den sist laddade vinner.
// Felet syns inte som ett fel, utan som en funktion som gör fel sak.
//
// Bakgrund: 2026-09-13 deklarerade både pm-tasklista.js och pm-forslag.js en
// sparaTasklage. Tasklistans filter slutade sparas utan att något klagade.
func TestSkriptfilernaDelarIngaGlobalaNamn(t *testing.T) {
	filer, err := filepath.Glob("static/pm*.js")
	if err != nil {
		t.Fatal(err)
	}
	if len(filer) < 5 {
		t.Fatalf("hittade bara %d skriptfiler, glob-mönstret verkar fel", len(filer))
	}
	// Deklarationer i kolumn noll är globala. Allt indraget ligger i en
	// funktion eller ett block och kan inte krocka.
	//
	// Vad provet inte ser: en indragen function eller var i ett toppnivåblock
	// som ändå hissas globalt, en tilldelning till window.X, och andra namnet
	// i en rad som "let a, b". De fallen finns inte i dag, men vakten täcker
	// dem inte om de dyker upp.
	deklaration := regexp.MustCompile(`(?m)^(?:function|const|let|var)\s+([A-Za-z_$][\w$]*)`)
	agare := map[string]string{}
	for _, fil := range filer {
		if strings.HasSuffix(fil, ".test.js") {
			continue
		}
		data, err := os.ReadFile(fil)
		if err != nil {
			t.Fatal(err)
		}
		for _, traff := range deklaration.FindAllSubmatch(data, -1) {
			namn := string(traff[1])
			if tidigare, finns := agare[namn]; finns {
				t.Errorf("%s deklareras både i %s och %s, den sist laddade vinner",
					namn, filepath.Base(tidigare), filepath.Base(fil))
				continue
			}
			agare[namn] = fil
		}
	}
}
