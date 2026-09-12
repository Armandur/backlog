package pm

// MaskeratVarde står i stället för en hemlighet när PM skickar konfigurationen
// utanför servern. Kommer samma värde tillbaka vid en sparning betyder det att
// användaren lämnade hemligheten orörd.
const MaskeratVarde = "***sparad***"

// Maskera ger en kopia av konfigurationen där varje miljövärde är utbytt mot
// MaskeratVarde. Nycklarna finns kvar, så konfigvyn kan visa vilka som är satta.
func (k Konfig) Maskera() Konfig {
	kopia := k
	kopia.Agenter = make(map[string]AgentKonfig, len(k.Agenter))
	for namn, agent := range k.Agenter {
		agent.Miljo = maskeraMiljo(agent.Miljo)
		kopia.Agenter[namn] = agent
	}
	kopia.Testserver = make(map[string]TestserverKonfig, len(k.Testserver))
	for namn, server := range k.Testserver {
		server.Miljo = maskeraMiljo(server.Miljo)
		kopia.Testserver[namn] = server
	}
	kopia.Krok.Miljo = maskeraMiljo(k.Krok.Miljo)
	return kopia
}

// AterstallMaskerat byter tillbaka varje maskerat värde mot det som redan
// ligger sparat. Saknas nyckeln där faller den bort, annars hade PM sparat
// maskeringen som om den vore en hemlighet.
func (k Konfig) AterstallMaskerat(sparad Konfig) Konfig {
	kopia := k
	kopia.Agenter = make(map[string]AgentKonfig, len(k.Agenter))
	for namn, agent := range k.Agenter {
		agent.Miljo = aterstallMiljo(agent.Miljo, sparad.Agenter[namn].Miljo)
		kopia.Agenter[namn] = agent
	}
	kopia.Testserver = make(map[string]TestserverKonfig, len(k.Testserver))
	for namn, server := range k.Testserver {
		server.Miljo = aterstallMiljo(server.Miljo, sparad.Testserver[namn].Miljo)
		kopia.Testserver[namn] = server
	}
	kopia.Krok.Miljo = aterstallMiljo(k.Krok.Miljo, sparad.Krok.Miljo)
	return kopia
}

func maskeraMiljo(miljo map[string]string) map[string]string {
	if miljo == nil {
		return nil
	}
	ut := make(map[string]string, len(miljo))
	for nyckel := range miljo {
		ut[nyckel] = MaskeratVarde
	}
	return ut
}

func aterstallMiljo(miljo, sparad map[string]string) map[string]string {
	if miljo == nil {
		return nil
	}
	ut := make(map[string]string, len(miljo))
	for nyckel, varde := range miljo {
		if varde != MaskeratVarde {
			ut[nyckel] = varde
			continue
		}
		if tidigare, finns := sparad[nyckel]; finns {
			ut[nyckel] = tidigare
		}
	}
	return ut
}
