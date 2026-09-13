package pm

// Filverb är inledningsorden PM sätter på en filhändelse. Webben delar upp
// texten på dem för att hitta sökvägen, se FILVERB i pm-samtal.js. Läggs ett
// verb till här utan att webben följer med, faller länken tyst bort. Provet i
// handelsefil_test.go jämför de två listorna.
var Filverb = []string{"ändrade", "skapade", "tog bort", "hanterade", "läste", "sökte i"}
