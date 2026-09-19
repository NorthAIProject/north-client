package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/NorthAIProject/north-client/internal/config"
	"github.com/NorthAIProject/north-client/internal/push"
	"github.com/NorthAIProject/north-client/internal/shared/database"
	"github.com/NorthAIProject/north-client/internal/spend"
	"github.com/NorthAIProject/north-client/internal/users"
	"github.com/joho/godotenv"
)

func runCommand(args []string) error {
	if len(args) == 0 {
		return run()
	}

	switch args[0] {
	case "migrate":
		return runMigrate()
	case "tier":
		return runTier(args[1:])
	case "spend":
		return runSpend(args[1:])
	case "simulate":
		return runSimulate(args[1:])
	case "telegram-check":
		return runTelegramCheck(args[1:])
	case "voice-check":
		return runVoiceCheck(args[1:])
	case "vapid-keygen":
		return runVAPIDKeygen()
	default:
		return run()
	}
}

// runVAPIDKeygen prints a key pair in the form .env and the SealedSecret take.
// It reads no configuration and opens no pool: there is nothing to check
// against, and a keygen that could fail on a missing DATABASE_URL would fail
// for the wrong reason.
func runVAPIDKeygen() error {
	public, private, err := push.GenerateKeys()
	if err != nil {
		return err
	}
	fmt.Printf("VAPID_PUBLIC_KEY=%s\nVAPID_PRIVATE_KEY=%s\n", public, private)
	return nil
}

// runMigrate applies pending migrations and returns. It deliberately does not
// open the application pool or build any service: a migration hook that can
// fail on an unrelated dependency is a migration hook that blocks deploys for
// the wrong reason.
func runMigrate() error {
	_ = godotenv.Load()

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	log := newLogger(cfg)
	slog.SetDefault(log)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := database.Migrate(ctx, cfg.DatabaseURL); err != nil {
		return fmt.Errorf("database migrations: %w", err)
	}
	log.Info("database migrations applied")

	return nil
}

// runTier changes one account's plan and reports what it did.
func runTier(args []string) error {
	if len(args) != 2 {
		return fmt.Errorf("usage: main tier <email> %s", strings.Join(tierNames(), "|"))
	}
	email, want := args[0], users.Tier(strings.TrimSpace(args[1]))
	if !want.Valid() {
		return fmt.Errorf("unknown tier %q; want one of %s", args[1], strings.Join(tierNames(), ", "))
	}

	_ = godotenv.Load()

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	slog.SetDefault(newLogger(cfg))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := database.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	svc := users.NewService(users.NewRepository(pool))

	user, err := svc.ByEmail(ctx, email)
	if err != nil {
		return fmt.Errorf("look up %s: %w", email, err)
	}
	if user.Tier == want {
		fmt.Printf("%s is already on %s\n", user.Email, want)
		return nil
	}

	was := user.Tier
	user, err = svc.UpdateTier(ctx, user.ID, want)
	if err != nil {
		return fmt.Errorf("update tier: %w", err)
	}

	fmt.Printf("%s moved from %s to %s\n", user.Email, was, user.Tier)
	return nil
}

// tierNames lists the tiers for a usage message, so adding one does not leave
// the help text behind.
func tierNames() []string {
	out := make([]string, 0, len(users.Tiers))
	for _, t := range users.Tiers {
		out = append(out, string(t))
	}
	return out
}

// runSpend prints what the model calls in a window cost.
func runSpend(args []string) error {
	fs := flag.NewFlagSet("spend", flag.ContinueOnError)
	var (
		from    = fs.String("from", "", "start date, inclusive (YYYY-MM-DD)")
		to      = fs.String("to", "", "end date, exclusive (YYYY-MM-DD)")
		email   = fs.String("user", "", "restrict to one account, by email")
		withOwn = fs.Bool("include-byok", false, "count spend paid for by users' own keys")
	)
	if err := fs.Parse(args); err != nil {
		return err
	}

	window, err := parseWindow(*from, *to)
	if err != nil {
		return err
	}

	_ = godotenv.Load()

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	slog.SetDefault(newLogger(cfg))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := database.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	repo := spend.NewRepository(pool)
	billableOnly := !*withOwn

	// Reported first, not last. A missing price makes every total below it an
	// understatement, and a number that is quietly wrong is worse than one
	// that is obviously incomplete.
	unpriced, err := repo.CountUnpriced(ctx, window)
	if err != nil {
		return err
	}
	if unpriced > 0 {
		fmt.Printf("WARNING: %d call(s) had no price. Totals below are understated.\n"+
			"Fill the model in at internal/ai/pricing/pricing.json.\n\n", unpriced)
	}

	fmt.Printf("%s to %s", window.From.Format(time.DateOnly), window.To.Format(time.DateOnly))
	if billableOnly {
		fmt.Print("  (excluding BYOK)")
	}
	fmt.Print("\n\n")

	bySurface, err := repo.BySurface(ctx, window, billableOnly)
	if err != nil {
		return err
	}
	fmt.Println("BY SURFACE")
	for _, row := range bySurface {
		fmt.Printf("  %-22s %8d calls  %12s\n", row.Surface, row.Generations, spend.Euros(row.CostMicros))
	}

	byModel, err := repo.ByModel(ctx, window, billableOnly)
	if err != nil {
		return err
	}
	fmt.Println("\nBY MODEL")
	for _, row := range byModel {
		name := row.Model
		if name == "" {
			name = "(model not reported)"
		}
		fmt.Printf("  %-22s %-34s %8d calls  %12s\n",
			row.Provider, name, row.Generations, spend.Euros(row.CostMicros))
	}

	byUser, err := repo.ByUser(ctx, window, billableOnly)
	if err != nil {
		return err
	}

	userSvc := users.NewService(users.NewRepository(pool))
	var total int64

	fmt.Println("\nBY ACCOUNT")
	for _, row := range byUser {
		total += row.CostMicros

		label := "(unattributed)"
		if row.UserID != nil {
			if u, uErr := userSvc.ByID(ctx, *row.UserID); uErr == nil {
				label = u.Email
			} else {
				label = row.UserID.String()
			}
		}
		if *email != "" && label != *email {
			continue
		}
		fmt.Printf("  %-38s %8d calls  %12s\n", label, row.Generations, spend.Euros(row.CostMicros))
	}

	fmt.Printf("\nTOTAL %s\n", spend.Euros(total))
	return nil
}

// parseWindow defaults to the last 30 days, which is the question anyone
// running this is usually asking.
func parseWindow(from, to string) (spend.Range, error) {
	now := time.Now().UTC()
	window := spend.Range{From: now.AddDate(0, 0, -30), To: now.AddDate(0, 0, 1)}

	if from != "" {
		t, err := time.Parse(time.DateOnly, from)
		if err != nil {
			return window, fmt.Errorf("--from must be YYYY-MM-DD: %w", err)
		}
		window.From = t
	}
	if to != "" {
		t, err := time.Parse(time.DateOnly, to)
		if err != nil {
			return window, fmt.Errorf("--to must be YYYY-MM-DD: %w", err)
		}
		window.To = t
	}
	if !window.To.After(window.From) {
		return window, fmt.Errorf("--to (%s) must be after --from (%s)",
			window.To.Format(time.DateOnly), window.From.Format(time.DateOnly))
	}
	return window, nil
}
