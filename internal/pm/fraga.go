package pm

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/mazen160/backlog/internal/models"
)

// FragaInput är en fråga till en agent i ett projektsamtal.
type FragaInput struct {
	Alias     string
	ProjectID string
	Fraga     string
	Agent     string
	Fragare   models.Actor
	Granser   KontextGranser
}

// Fraga sparar frågan i tråden, skickar den med projektets kontext till
// agenten och sparar svaret som ett ai-inlägg.
func Fraga(ctx context.Context, db *sql.DB, reg *AgentRegister, in FragaInput) (*Inlagg, error) {
	if reg == nil {
		return nil, fmt.Errorf("ingen agent är registrerad")
	}
	agent, err := reg.Hamta(in.Agent)
	if err != nil {
		return nil, err
	}
	store := NewSamtalStore(db)
	if _, err := store.Add(ctx, in.ProjectID, "", in.Fragare, in.Fraga); err != nil {
		return nil, err
	}
	kontext, err := ByggKontext(ctx, db, in.Alias, in.ProjectID, in.Granser)
	if err != nil {
		return nil, err
	}
	svar, err := agent.Fraga(ctx, ByggPrompt(kontext, in.Fraga))
	if err != nil {
		return nil, err
	}
	return store.Add(ctx, in.ProjectID, "", models.Actor{Kind: models.ActorKindAI, Name: agent.Namn()}, svar)
}
