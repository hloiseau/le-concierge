package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"github.com/hloiseau/le-concierge/internal/config"
	"github.com/hloiseau/le-concierge/internal/discord"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	if e := run(); e != nil {
		slog.Error("bot stopped", "error", e)
		os.Exit(1)
	}
}
func run() error {
	if len(os.Args) > 1 && os.Args[1] == "--healthcheck" {
		client := http.Client{Timeout: 3 * time.Second}
		r, e := client.Get("http://127.0.0.1:8081/healthz")
		if e != nil {
			return errors.New("unhealthy")
		}
		defer r.Body.Close()
		if r.StatusCode != 200 {
			return errors.New("unhealthy")
		}
		return nil
	}
	register := flag.Bool("register", false, "register this app's guild commands, then exit")
	publishInterests := flag.Bool("publish-interests", false, "publish the configured interest panel, then exit")
	messageID := flag.String("message-id", "", "update this bot's existing interest panel message instead of creating one")
	flag.Parse()
	messageIDSet := false
	flag.Visit(func(f *flag.Flag) {
		if f.Name == "message-id" {
			messageIDSet = true
		}
	})
	if (*register && *publishInterests) || (messageIDSet && (!*publishInterests || *messageID == "")) {
		return errors.New("--register and --publish-interests are exclusive; --message-id requires --publish-interests")
	}
	c, e := config.Load()
	if e != nil {
		return e
	}
	bot := discord.NewBot(c)
	if *publishInterests {
		id, err := bot.PublishInterests(context.Background(), *messageID)
		if err != nil {
			return err
		}
		fmt.Printf("Interest panel message: %s\n", id)
		return nil
	}
	if c.DiscordPublicKey == "" {
		return errors.New("configure Discord application, guild, public key and BOT_API_SECRET first")
	}
	if *register {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		return bot.Register(ctx)
	}
	srv := &http.Server{Addr: c.BotListen, Handler: bot.Handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 32 * 1024}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go registerOnStart(ctx, bot)
	done := make(chan error, 1)
	go func() {
		slog.Info("Discord interactions listening", "address", c.BotListen)
		done <- srv.ListenAndServe()
	}()
	select {
	case e := <-done:
		if !errors.Is(e, http.ErrServerClosed) {
			return e
		}
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if e = srv.Shutdown(shutdown); e != nil {
			return e
		}
		bot.Wait()
	}
	return nil
}

func registerOnStart(ctx context.Context, bot *discord.Bot) {
	if bot.Config.DiscordBotToken == "" {
		slog.Info("Discord command registration skipped: configure DISCORD_BOT_TOKEN or use --register")
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if err := bot.Register(ctx); err != nil {
		// Provider responses and transport errors may contain credentials.
		slog.Warn("Discord command registration failed; existing interactions remain available; retry with --register or restart")
		return
	}
	slog.Info("Discord guild commands updated")
}
