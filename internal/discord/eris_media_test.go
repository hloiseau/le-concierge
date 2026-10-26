package discord

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/bwmarrin/discordgo"
)

func erisPNGFixture(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			img.SetRGBA(x, y, color.RGBA{255, 0, 0, 255})
		}
	}
	var out bytes.Buffer
	if err := png.Encode(&out, img); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func TestErisMediaUsesVerifiedIdentityAndActualCDNImage(t *testing.T) {
	b, _ := signedCommand(t, "eris")
	b.Eris.V2, b.Eris.Protocol = true, "responses"
	m := mentionFixture().Message
	m.Author.Username, m.Author.GlobalName = "alice", "Global Alice"
	m.Member = &discordgo.Member{Nick: "Alice [BLEU]"}
	m.Mentions = append(m.Mentions, &discordgo.User{ID: "22222", Username: "bob", GlobalName: "Bob"})
	m.Attachments = []*discordgo.MessageAttachment{{URL: "https://cdn.discordapp.com/attachments/1/2/misleading.gif", ContentType: "image/png", Width: 8, Height: 8}}
	body := erisPNGFixture(t)
	requests := 0
	b.Client.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
		requests++
		if r.URL.Host != "cdn.discordapp.com" || r.Header.Get("Authorization") != "" {
			t.Error("credentials or wrong image destination")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(body)), Header: make(http.Header)}, nil
	})
	in := b.erisDiscordInput(context.Background(), nil, m)
	if in.Speaker.ID != m.Author.ID || in.Speaker.Name != "Alice [BLEU]" || len(in.Mentions) != 1 || in.Mentions[0].ID != "22222" || len(in.Images) != 1 || requests != 1 {
		t.Fatal(in.Speaker, in.Mentions, len(in.Images), requests)
	}
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(in.Images[0], "data:image/jpeg;base64,"))
	if err != nil {
		t.Fatal(err)
	}
	img, err := jpeg.Decode(bytes.NewReader(decoded))
	if err != nil {
		t.Fatal(err)
	}
	r, g, bb, _ := img.At(4, 4).RGBA()
	if r <= g || r <= bb {
		t.Fatal("image pixels changed")
	}
	if strings.Contains(in.VisualNote, "misleading") {
		t.Fatal("filename used as visual interpretation")
	}
}

func TestErisMediaRejectsArbitraryHostsRedirectsAndOversize(t *testing.T) {
	b, _ := signedCommand(t, "eris")
	requests := 0
	b.Client.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
		requests++
		if strings.Contains(r.URL.Path, "redirect") {
			return &http.Response{StatusCode: 302, Header: http.Header{"Location": []string{"https://media.discordapp.net/another.png"}}, Body: io.NopCloser(strings.NewReader(""))}, nil
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(strings.Repeat("x", 11)))}, nil
	})
	for _, raw := range []string{"http://cdn.discordapp.com/a.png", "https://127.0.0.1/a.png", "https://cdn.discordapp.com.evil.test/a.png", "https://user@cdn.discordapp.com/a.png", "https://cdn.discordapp.com:8080/a.png"} {
		if _, err := b.erisMediaBytes(context.Background(), raw, 10); err == nil {
			t.Fatal("accepted arbitrary image source", raw)
		}
	}
	if requests != 0 {
		t.Fatal("untrusted URL requested")
	}
	if _, err := b.erisMediaBytes(context.Background(), "https://cdn.discordapp.com/redirect.png", 10); err == nil {
		t.Fatal("redirect accepted")
	}
	if requests != 1 {
		t.Fatal("followed image redirect")
	}
	if _, err := b.erisMediaBytes(context.Background(), "https://cdn.discordapp.com/large.png", 10); err == nil {
		t.Fatal("unbounded image accepted")
	}
}

func TestErisLinkWithoutImageDoesNotBecomeASlugDescription(t *testing.T) {
	b, _ := signedCommand(t, "eris")
	b.Eris.V2, b.Eris.Protocol = true, "responses"
	m := mentionFixture().Message
	m.Content = "<@12345> https://klipy.com/gifs/football-label"
	m.Embeds = []*discordgo.MessageEmbed{{Type: discordgo.EmbedTypeGifv, Title: "Football", Description: "Arbitrary title", Thumbnail: &discordgo.MessageEmbedThumbnail{URL: "https://private.test/image.png"}}}
	b.Client.Transport = transportFunc(func(*http.Request) (*http.Response, error) { t.Error("requested untrusted URL"); return nil, nil })
	in := b.erisDiscordInput(context.Background(), nil, m)
	if len(in.Images) != 0 || !strings.Contains(in.VisualNote, "aucune image") || strings.Contains(in.VisualNote, "Football") {
		t.Fatal("guessed visual content from metadata", in.VisualNote)
	}
}

func TestErisRefreshesOnlyTheSameVerifiedMessageForDelayedEmbeds(t *testing.T) {
	for _, wrongAuthor := range []bool{false, true} {
		b, _ := signedCommand(t, "eris")
		b.Eris.V2, b.Eris.Protocol = true, "responses"
		m := mentionFixture().Message
		m.Content = "<@12345> https://klipy.com/gifs/reaction"
		s, _ := discordgo.New("Bot fixture")
		s.Client = &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
			if !strings.HasSuffix(r.URL.Path, "/channels/33333/messages/99999") {
				t.Error("read a different message", r.URL.Path)
			}
			author := m.Author.ID
			if wrongAuthor {
				author = "22222"
			}
			fresh := &discordgo.Message{ID: m.ID, ChannelID: m.ChannelID, Author: &discordgo.User{ID: author}, Embeds: []*discordgo.MessageEmbed{{Type: discordgo.EmbedTypeGifv, Thumbnail: &discordgo.MessageEmbedThumbnail{ProxyURL: "https://images-ext-1.discordapp.net/external/preview.png"}}}}
			raw, _ := json.Marshal(fresh)
			return botResponse(200, string(raw)), nil
		})}
		body := erisPNGFixture(t)
		b.Client.Transport = transportFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(body))}, nil
		})
		in := b.erisDiscordInput(context.Background(), s, m)
		if (len(in.Images) == 1) == wrongAuthor {
			t.Fatal("unverified refreshed message accepted", wrongAuthor, len(in.Images))
		}
	}
}

func TestErisSamplesGIFFramesAndBoundsAnimatedDecoding(t *testing.T) {
	palette := color.Palette{color.White, color.RGBA{255, 0, 0, 255}, color.RGBA{0, 255, 0, 255}, color.RGBA{0, 0, 255, 255}}
	g := &gif.GIF{Config: image.Config{ColorModel: palette, Width: 8, Height: 8}, Delay: []int{1, 1, 1}}
	for i := 1; i <= 3; i++ {
		frame := image.NewPaletted(image.Rect(0, 0, 8, 8), palette)
		for p := range frame.Pix {
			frame.Pix[p] = byte(i)
		}
		g.Image = append(g.Image, frame)
	}
	var raw bytes.Buffer
	if err := gif.EncodeAll(&raw, g); err != nil {
		t.Fatal(err)
	}
	frames, note, err := erisImageFrames(raw.Bytes())
	if err != nil || len(frames) != 3 || !strings.Contains(note, "extraits") {
		t.Fatal(len(frames), note, err)
	}
	for i, data := range frames {
		body, _ := base64.StdEncoding.DecodeString(strings.TrimPrefix(data, "data:image/jpeg;base64,"))
		img, err := jpeg.Decode(bytes.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		r, g, b, _ := img.At(4, 4).RGBA()
		channels := []uint32{r, g, b}
		if channels[i] < 50000 {
			t.Fatal("wrong GIF sample order", i, channels)
		}
	}
	g.Image = nil
	g.Delay = nil
	for i := 0; i < 100; i++ {
		g.Image = append(g.Image, image.NewPaletted(image.Rect(0, 0, 8, 8), palette))
		g.Delay = append(g.Delay, 1)
	}
	raw.Reset()
	if err := gif.EncodeAll(&raw, g); err != nil {
		t.Fatal(err)
	}
	if erisBoundedGIF(raw.Bytes()) {
		t.Fatal("excessive frame count accepted")
	}
	frames, note, err = erisImageFrames(raw.Bytes())
	if err != nil || len(frames) != 1 || !strings.Contains(note, "première") {
		t.Fatal("no bounded first-frame fallback", len(frames), note, err)
	}
	if _, _, err = erisImageFrames([]byte("<html>not an image</html>")); err == nil {
		t.Fatal("accepted HTML as visual content")
	}
}
