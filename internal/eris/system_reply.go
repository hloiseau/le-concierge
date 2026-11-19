package eris

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"unicode/utf8"
)

// Explain turns a verified application outcome into Eris's own words, without
// executing tools or loading a person's history to narrate a system result.
func (c *Chat) Explain(ctx context.Context, question string, facts any) (string, error) {
	data, err := json.Marshal(struct {
		Message string `json:"message"`
		Result  any    `json:"result"`
	}{question, facts})
	if err != nil || c.URL == "" {
		return "", ErrUnavailable
	}
	instructions := c.instructions(false, false) + `
Le JSON fourni décrit le résultat réel d'une opération de l'application. Rédige ta réponse dans ta voix, avec seulement ces faits. Un échec ou un état inconnu ne devient pas un succès. N'invente aucune action, souvenir, réparation ou suppression. Aucun outil ni mémoire n'est disponible. Produis uniquement {"text":"ta réponse"}, sans formule imposée, en 800 caractères maximum.`
	raw, err := c.complete(ctx, c.dialoguePrompt([]Message{{Role: "system", Content: instructions}, {Role: "user", Content: string(data)}}), true, completionBudget{Tokens: 600, Runes: 1500})
	if err != nil {
		return "", err
	}
	var answer struct {
		Text string `json:"text"`
	}
	d := json.NewDecoder(strings.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&answer) != nil || d.Decode(new(any)) != io.EOF || strings.TrimSpace(answer.Text) == "" || utf8.RuneCountInString(answer.Text) > 800 {
		return "", ErrUnavailable
	}
	return answer.Text, nil
}
