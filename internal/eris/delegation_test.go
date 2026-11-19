package eris

import "testing"

func TestNativeMediaToolsAcceptAnExplicitRecipient(t *testing.T) {
	for name, args := range map[string]string{
		"request_media": `{"mediaId":270946,"kind":"movie","targetDiscordId":"22222"}`,
		"status":        `{"targetDiscordId":"22222"}`,
	} {
		call := ToolCall{ID: "delegate", Type: "function"}
		call.Function.Name, call.Function.Arguments = name, args
		if _, err := agentPlan(call); err != nil {
			t.Errorf("%s cannot represent Keftcha as the recipient: %v", name, err)
		}
	}
}

func TestDelegationCannotTargetPrivateToolsOrUseAnUnverifiedName(t *testing.T) {
	for _, raw := range []string{
		`{"action":"preferences","targetDiscordId":"22222"}`,
		`{"action":"gifts","targetDiscordId":"22222"}`,
		`{"action":"recommend","unwatched":true,"targetDiscordId":"22222"}`,
		`{"action":"status","targetDiscordId":"Keftcha"}`,
	} {
		if _, err := ParsePlan(raw); err == nil {
			t.Fatal("invalid delegation accepted", raw)
		}
	}
	call := ToolCall{ID: "missing-recipient", Type: "function"}
	call.Function.Name = "request_media"
	for _, args := range []string{`{"mediaId":270946,"kind":"movie"}`, `{"mediaId":270946,"kind":"movie","targetDiscordId":""}`, `{"mediaId":270946,"kind":"movie","targetDiscordId":null}`} {
		call.Function.Arguments = args
		if _, err := agentPlan(call); err == nil {
			t.Fatal("model did not explicitly choose a recipient", args)
		}
	}
}
