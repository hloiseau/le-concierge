// eris-lab keeps a real conversation open over JSON lines, with simulated tools.
package main

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/hloiseau/le-concierge/internal/eris"
)

type input struct {
	Speaker  string `json:"speaker"`
	Text     string `json:"text"`
	Ambient  bool   `json:"ambient,omitempty"`
	Explicit bool   `json:"explicit,omitempty"`
	Forget   bool   `json:"forget,omitempty"`
}

type output struct {
	Ready       bool       `json:"ready,omitempty"`
	Model       string     `json:"model,omitempty"`
	PersonaHash string     `json:"persona_sha256,omitempty"`
	Revision    string     `json:"revision,omitempty"`
	Speaker     string     `json:"speaker,omitempty"`
	Text        string     `json:"text,omitempty"`
	Silent      bool       `json:"silent,omitempty"`
	Observed    bool       `json:"observed,omitempty"`
	Tool        *eris.Plan `json:"simulated_tool,omitempty"`
	Error       string     `json:"error,omitempty"`
}

func main() {
	if err := start(); err != nil {
		fmt.Fprintln(os.Stderr, "Eris lab stopped; check configuration, input and output.")
		os.Exit(1)
	}
}

func start() error {
	model, key := os.Getenv("ERIS_MODEL"), os.Getenv("ERIS_API_KEY_FILE")
	if model == "" || key == "" {
		return errors.New("model and key file required")
	}
	dir, err := os.MkdirTemp("", "eris-lab-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	memory, err := eris.OpenMemory(filepath.Join(dir, "memory.db"))
	if err != nil {
		return err
	}
	defer memory.Close()
	c := eris.New("https://api.openai.com/v1", "", model)
	c.V2, c.Protocol, c.KeyFile, c.Memory = true, "responses", key, memory
	return converse(os.Stdin, os.Stdout, c)
}

func converse(reader io.Reader, writer io.Writer, c *eris.Chat) error {
	encoder := json.NewEncoder(writer)
	hash := sha256.Sum256([]byte(eris.CharacterPersona + eris.CharacterCanon))
	if err := encoder.Encode(output{Ready: true, Model: c.Model, PersonaHash: hex.EncodeToString(hash[:]), Revision: os.Getenv("ERIS_LAB_REVISION")}); err != nil {
		return err
	}
	speakers := map[string]string{}
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 4096), 8192)
	for turn := 1; scanner.Scan(); turn++ {
		var request input
		decoder := json.NewDecoder(strings.NewReader(scanner.Text()))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&request); err != nil || decoder.Decode(new(any)) != io.EOF ||
			utf8.RuneCountInString(request.Text) > 500 || (!request.Forget && strings.TrimSpace(request.Text) == "") ||
			(request.Forget && (request.Text != "" || request.Ambient)) || (request.Ambient && request.Explicit) ||
			utf8.RuneCountInString(request.Speaker) > 80 || strings.ContainsFunc(request.Speaker, unicode.IsControl) {
			if err := encoder.Encode(output{Error: "invalid_input"}); err != nil {
				return err
			}
			continue
		}
		name := strings.TrimSpace(request.Speaker)
		if name == "" {
			name = "Hugo"
		}
		member := speakers[name]
		if member == "" {
			if len(speakers) >= 32 {
				if err := encoder.Encode(output{Error: "speaker_limit"}); err != nil {
					return err
				}
				continue
			}
			member = fmt.Sprintf("%05d", 20000+len(speakers))
			speakers[name] = member
		}
		owner := "10000:" + member + ":30000"
		incoming := eris.DiscordInput{MessageID: fmt.Sprintf("%05d", 40000+turn), Speaker: eris.Speaker{ID: member, Name: name, Username: "lab_" + member}}
		result := output{Speaker: name}
		var err error
		switch {
		case request.Forget:
			result.Text, err = c.Reply(context.Background(), owner, "", true)
		case request.Ambient:
			c.ObserveAmbient(owner, request.Text, incoming)
			result.Observed = true
		case c.SkipPublicFollowup(owner, request.Text, request.Explicit):
			result.Silent = true
		default:
			result.Text, err = c.ReplyWithDiscordContext(context.Background(), owner, request.Text, false, "", incoming, func(_ context.Context, plan eris.Plan) (eris.ToolReply, error) {
				result.Tool = &plan
				return eris.ToolReply{Content: "Outil simulé : aucune action exécutée, aucune donnée réelle consultée."}, nil
			})
			result.Silent = err == nil && result.Text == ""
		}
		if errors.Is(err, eris.ErrBusy) {
			result.Error = "busy"
		} else if err != nil {
			result.Error = "unavailable"
		}
		if err := encoder.Encode(result); err != nil {
			return err
		}
		if result.Error == "" && result.Text != "" && !request.Forget {
			c.Delivered(owner, fmt.Sprintf("%05d", 50000+turn), result.Text)
		}
	}
	return scanner.Err()
}
