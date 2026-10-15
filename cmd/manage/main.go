package main

import (
	"context"
	"flag"
	"fmt"
	"github.com/hloiseau/le-concierge/internal/config"
	"github.com/hloiseau/le-concierge/internal/store"
	"os"
	"time"
)

func main() {
	if e := run(); e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}
func run() error {
	if len(os.Args) < 2 {
		return fmt.Errorf("usage: manage link-seerr|unlink|backup [flags]")
	}
	command := os.Args[1]
	f := flag.NewFlagSet(command, flag.ContinueOnError)
	dbpath := f.String("database", os.Getenv("DATABASE_PATH"), "SQLite database path")
	id := f.String("discord", "", "verified Discord user ID")
	seerr := f.Int64("seerr", 0, "verified Seerr user ID")
	dest := f.String("destination", "", "new backup file")
	if e := f.Parse(os.Args[2:]); e != nil {
		return e
	}
	if *dbpath == "" {
		*dbpath = "data/concierge.db"
	}
	if command != "link-seerr" && command != "unlink" && command != "backup" {
		return fmt.Errorf("unknown command")
	}
	if command != "backup" && !config.Snowflake(*id) {
		return fmt.Errorf("valid --discord ID required")
	}
	if command == "backup" && *dest == "" {
		return fmt.Errorf("--destination required")
	}
	if _, e := os.Stat(*dbpath); e != nil {
		return fmt.Errorf("database does not exist; start API first")
	}
	db, e := store.Open(*dbpath)
	if e != nil {
		return e
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if command != "backup" {
		u, err := db.UserByDiscord(ctx, *id)
		if err != nil {
			return fmt.Errorf("Discord user not found")
		}
		*id = u.ID
	}
	switch command {
	case "link-seerr":
		e = db.LinkSeerr(ctx, *id, *seerr)
	case "unlink":
		e = db.Unlink(ctx, *id)
	case "backup":
		e = db.Backup(ctx, *dest)
	}
	if e == nil {
		fmt.Println("Operation completed.")
	}
	return e
}
