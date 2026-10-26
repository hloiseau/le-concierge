package discord

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"image"
	"image/color"
	"image/draw"
	"image/gif"
	"image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/hloiseau/le-concierge/internal/eris"
	_ "golang.org/x/image/webp"
)

const maxErisMediaBytes = 3 * 1024 * 1024

func discordSpeaker(id, nick, global, username string) eris.Speaker {
	name := nick
	if name == "" {
		name = global
	}
	if name == "" {
		name = username
	}
	return eris.Speaker{ID: id, Name: name, Username: username}
}

type erisMediaCandidate struct {
	url     string
	preview bool
}

func erisMediaURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Fragment != "" || (u.Port() != "" && u.Port() != "443") {
		return false
	}
	switch strings.ToLower(u.Hostname()) {
	case "cdn.discordapp.com", "media.discordapp.net", "images-ext-1.discordapp.net", "images-ext-2.discordapp.net":
		return true
	}
	return false
}

func mediaLink(text string) bool {
	for _, word := range strings.Fields(text) {
		u, err := url.Parse(strings.Trim(word, "<>.,!?()"))
		if err != nil || u.Scheme != "https" {
			continue
		}
		switch strings.ToLower(u.Hostname()) {
		case "klipy.com", "www.klipy.com", "tenor.com", "www.tenor.com", "giphy.com", "www.giphy.com", "media.giphy.com":
			return true
		}
		path := strings.ToLower(u.Path)
		for _, ext := range []string{".gif", ".webp", ".png", ".jpg", ".jpeg", ".mp4"} {
			if strings.HasSuffix(path, ext) {
				return true
			}
		}
	}
	return false
}

func erisMediaCandidates(m *discordgo.Message) []erisMediaCandidate {
	var out []erisMediaCandidate
	add := func(proxy, original string, preview bool) {
		raw := proxy
		if !erisMediaURL(raw) {
			raw = original
		}
		if !erisMediaURL(raw) {
			return
		}
		for _, old := range out {
			if old.url == raw {
				return
			}
		}
		out = append(out, erisMediaCandidate{raw, preview})
	}
	for _, a := range m.Attachments {
		if a == nil || a.Ephemeral || a.Size > maxErisMediaBytes {
			continue
		}
		if strings.HasPrefix(a.ContentType, "image/") || a.Width > 0 {
			add(a.ProxyURL, a.URL, false)
		}
	}
	for _, e := range m.Embeds {
		if e == nil || (e.Type != discordgo.EmbedTypeGifv && e.Type != discordgo.EmbedTypeImage && e.Type != discordgo.EmbedTypeVideo && !mediaLink(m.Content)) {
			continue
		}
		if e.Image != nil {
			add(e.Image.ProxyURL, e.Image.URL, e.Type == discordgo.EmbedTypeGifv || e.Type == discordgo.EmbedTypeVideo)
		}
		if e.Thumbnail != nil {
			add(e.Thumbnail.ProxyURL, e.Thumbnail.URL, true)
		}
	}
	return out
}

func (b *Bot) erisDiscordInput(ctx context.Context, session *discordgo.Session, m *discordgo.Message) eris.DiscordInput {
	nick := ""
	if m.Member != nil {
		nick = m.Member.Nick
	}
	in := eris.DiscordInput{MessageID: m.ID, Speaker: discordSpeaker(m.Author.ID, nick, m.Author.GlobalName, m.Author.Username)}
	question, _ := ErisQuestion(m, b.Config.DiscordClientID)
	in.MediaOnly = len(strings.Fields(question)) == 1 && mediaLink(question)
	for _, u := range m.Mentions {
		if u != nil && u.ID != b.Config.DiscordClientID {
			in.Mentions = append(in.Mentions, discordSpeaker(u.ID, "", u.GlobalName, u.Username))
		}
	}
	visual := len(m.Attachments) > 0 || mediaLink(m.Content)
	for _, e := range m.Embeds {
		visual = visual || (e != nil && (e.Type == discordgo.EmbedTypeGifv || e.Type == discordgo.EmbedTypeImage || e.Type == discordgo.EmbedTypeVideo))
	}
	if !visual {
		return in
	}
	in.VisualNote = "Un média a été envoyé, mais aucune image n'est accessible. Son URL et ses métadonnées ne permettent pas d'en connaître le contenu."
	if !b.Eris.V2 || b.Eris.Protocol != "responses" {
		return in
	}
	candidates := erisMediaCandidates(m)
	// Discord may populate a link preview after MESSAGE_CREATE. Re-read only the
	// addressed message once; never crawl the linked website or the channel.
	if len(candidates) == 0 && mediaLink(m.Content) && session != nil {
		timer := time.NewTimer(350 * time.Millisecond)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return in
		case <-timer.C:
		}
		fresh, err := session.ChannelMessage(m.ChannelID, m.ID, discordgo.WithContext(ctx))
		if err == nil && fresh != nil && fresh.ID == m.ID && fresh.ChannelID == m.ChannelID &&
			(fresh.GuildID == "" || fresh.GuildID == m.GuildID) && fresh.Author != nil && fresh.Author.ID == m.Author.ID && !fresh.Author.Bot && fresh.WebhookID == "" {
			candidates = erisMediaCandidates(fresh)
		}
	}
	used := 0
	for i, candidate := range candidates {
		if i >= 2 || len(in.Images) >= 3 || used >= maxErisMediaBytes {
			break
		}
		body, err := b.erisMediaBytes(ctx, candidate.url, maxErisMediaBytes-used)
		if err != nil {
			continue
		}
		used += len(body)
		frames, note, err := erisImageFrames(body)
		if err != nil {
			continue
		}
		if candidate.preview && len(frames) == 1 {
			note = "Une vignette du média est visible ; l'animation complète n'est pas analysée."
		}
		before := len(in.Images)
		for _, frame := range frames {
			if len(in.Images) < 3 {
				in.Images = append(in.Images, frame)
			}
		}
		if before == 0 {
			in.VisualNote = note
		} else {
			in.VisualNote += " " + note
		}
	}
	return in
}

func (b *Bot) erisMediaBytes(ctx context.Context, raw string, limit int) ([]byte, error) {
	if !erisMediaURL(raw) || limit <= 0 {
		return nil, errors.New("unsupported image source")
	}
	client := *b.Client
	client.Timeout = 4 * time.Second
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	req, err := http.NewRequestWithContext(ctx, "GET", raw, nil)
	if err != nil {
		return nil, errors.New("invalid image source")
	}
	req.Header.Set("Accept", "image/*")
	resp, err := client.Do(req)
	if err != nil {
		return nil, errors.New("image unavailable")
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 || resp.ContentLength > int64(limit) {
		return nil, errors.New("image unavailable")
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, int64(limit)+1))
	if err != nil || len(body) > limit {
		return nil, errors.New("image too large")
	}
	return body, nil
}

func erisImageFrames(body []byte) ([]string, string, error) {
	invalid := errors.New("unsupported image")
	if len(body) > maxErisMediaBytes {
		return nil, "", invalid
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(body))
	if err != nil || cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width > 2048 || cfg.Height > 2048 || cfg.Width*cfg.Height > 1024*1024 {
		return nil, "", invalid
	}
	if format != "gif" {
		img, _, err := image.Decode(bytes.NewReader(body))
		if err != nil {
			return nil, "", invalid
		}
		return []string{erisJPEG(img)}, "Une image est effectivement jointe à ce message.", nil
	}
	// Preflight compressed GIF structure before DecodeAll can allocate every frame.
	if !erisBoundedGIF(body) {
		first, err := gif.Decode(bytes.NewReader(body))
		if err != nil {
			return nil, "", invalid
		}
		return []string{erisJPEG(first)}, "Seule la première image du GIF est visible ; le reste de l'animation n'est pas analysé.", nil
	}
	g, err := gif.DecodeAll(bytes.NewReader(body))
	if err != nil || len(g.Image) == 0 {
		return nil, "", invalid
	}
	canvas := image.NewRGBA(image.Rect(0, 0, cfg.Width, cfg.Height))
	draw.Draw(canvas, canvas.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	frames := []string{}
	for i, frame := range g.Image {
		var previous *image.RGBA
		if len(g.Disposal) > i && g.Disposal[i] == gif.DisposalPrevious {
			previous = image.NewRGBA(canvas.Bounds())
			copy(previous.Pix, canvas.Pix)
		}
		draw.Draw(canvas, frame.Bounds(), frame, frame.Bounds().Min, draw.Over)
		if i == 0 || i == len(g.Image)/2 || i == len(g.Image)-1 {
			frames = append(frames, erisJPEG(canvas))
		}
		if len(g.Disposal) > i {
			switch g.Disposal[i] {
			case gif.DisposalBackground:
				draw.Draw(canvas, frame.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
			case gif.DisposalPrevious:
				if previous != nil {
					canvas = previous
				}
			}
		}
	}
	note := "Images effectivement extraites du début, du milieu et de la fin d'un GIF ; ce sont des extraits, pas toute l'animation."
	if len(g.Image) == 1 {
		note = "Une image GIF non animée est effectivement jointe."
	}
	return frames, note, nil
}

func erisBoundedGIF(body []byte) bool {
	if len(body) < 13 {
		return false
	}
	p := 13
	if body[10]&0x80 != 0 {
		p += 3 * (1 << uint((body[10]&7)+1))
	}
	frames, pixels := 0, 0
	skipBlocks := func() bool {
		for p < len(body) {
			n := int(body[p])
			p++
			if n == 0 {
				return true
			}
			p += n
			if p > len(body) {
				return false
			}
		}
		return false
	}
	for p < len(body) {
		kind := body[p]
		p++
		switch kind {
		case 0x3b:
			return frames > 0
		case 0x21:
			if p >= len(body) {
				return false
			}
			p++
			if !skipBlocks() {
				return false
			}
		case 0x2c:
			if p+9 > len(body) {
				return false
			}
			w, h := int(binary.LittleEndian.Uint16(body[p+4:p+6])), int(binary.LittleEndian.Uint16(body[p+6:p+8]))
			pixels += w * h
			frames++
			if w == 0 || h == 0 || frames > 96 || pixels > 8*1024*1024 {
				return false
			}
			packed := body[p+8]
			p += 9
			if packed&0x80 != 0 {
				p += 3 * (1 << uint((packed&7)+1))
			}
			if p >= len(body) {
				return false
			}
			p++
			if !skipBlocks() {
				return false
			}
		default:
			return false
		}
	}
	return false
}

func erisJPEG(src image.Image) string {
	bounds := src.Bounds()
	w, h := bounds.Dx(), bounds.Dy()
	if w > 512 || h > 512 {
		if w >= h {
			h = max(1, h*512/w)
			w = 512
		} else {
			w = max(1, w*512/h)
			h = 512
		}
	}
	out := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(out, out.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			c := src.At(bounds.Min.X+x*bounds.Dx()/w, bounds.Min.Y+y*bounds.Dy()/h)
			r, g, b, a := c.RGBA()
			out.SetRGBA(x, y, color.RGBA{uint8((r + 0xffff - a) >> 8), uint8((g + 0xffff - a) >> 8), uint8((b + 0xffff - a) >> 8), 255})
		}
	}
	var encoded bytes.Buffer
	_ = jpeg.Encode(&encoded, out, &jpeg.Options{Quality: 75})
	return "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(encoded.Bytes())
}
