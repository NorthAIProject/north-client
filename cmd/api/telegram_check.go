package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/joho/godotenv"

	"github.com/NorthAIProject/north-client/internal/config"
	"github.com/NorthAIProject/north-client/internal/messaging"
	"github.com/NorthAIProject/north-client/internal/messaging/telegram"
)

// runTelegramCheck proves that this deployment can actually talk to Telegram.
//
// It exists because the adapter has always been in the state docs/gateways.md
// calls "implemented and unproven": every test runs against an httptest stand-in
// for the Bot API, which proves the adapter is correct and cannot prove a bot
// works, because until somebody creates one none exists. The difference is
// invisible from a green build, and this is the command that closes it.
//
// Read-only by default. It asks Telegram three questions and changes nothing, so
// it is safe to run against production; --register-commands and --send-to are
// the two flags that write, and both say so.
//
// No database, no pool, no services: a token check that can fail on an unrelated
// dependency is a token check that answers the wrong question.
func runTelegramCheck(args []string) error {
	fs := flag.NewFlagSet("telegram-check", flag.ContinueOnError)
	sendTo := fs.String("send-to", "", "chat id to send a test message to (writes; find it by messaging the bot first)")
	registerCommands := fs.Bool("register-commands", false, "publish the /start, /help and /unlink menu (writes)")
	timeout := fs.Duration("timeout", 20*time.Second, "how long to allow for the whole check")

	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, `usage: main telegram-check [flags]

Verifies TELEGRAM_BOT_TOKEN against the real Bot API. Read-only unless
--register-commands or --send-to is given.

flags:
`)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}

	_ = godotenv.Load()

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if !cfg.Telegram.Enabled() {
		return errors.New("TELEGRAM_BOT_TOKEN is not set, so there is nothing to check; get a token from @BotFather")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, *timeout)
	defer cancel()

	client := telegram.NewClient(cfg.Telegram.BotToken)

	// 1. Who does this token belong to?
	info, err := client.GetMe(ctx)
	if err != nil {
		return fmt.Errorf("getMe failed, so the token cannot reach Telegram: %w", err)
	}
	fmt.Printf("token ok        @%s (%s, id %d)\n", info.Username, info.FirstName, info.ID)

	// The configured username is cosmetic — it is what Settings links people to
	// — but a mismatch means somebody is being sent to the wrong bot, which
	// looks exactly like the link code being broken.
	if configured := cfg.Telegram.BotUsername; configured != "" && !strings.EqualFold(configured, info.Username) {
		fmt.Printf("MISMATCH        TELEGRAM_BOT_USERNAME is %q but the token belongs to @%s;\n"+
			"                Settings would send people to the wrong bot\n", configured, info.Username)
	}

	// 2. Where does Telegram think it should deliver updates?
	hook, err := client.GetWebhookInfo(ctx)
	if err != nil {
		return fmt.Errorf("getWebhookInfo failed: %w", err)
	}
	reportDeliveryMode(cfg.Telegram, hook)

	// 3. Optional writes.
	if *registerCommands {
		if err := client.RegisterCommands(ctx); err != nil {
			return fmt.Errorf("setMyCommands failed: %w", err)
		}
		fmt.Println("commands ok     /start, /help and /unlink are published")
	}

	if *sendTo != "" {
		if err := client.Send(ctx, *sendTo, messaging.OutboundMessage{
			Text: "North is connected. This is a test message from `main telegram-check`.",
		}); err != nil {
			return fmt.Errorf("sendMessage to %s failed: %w", *sendTo, err)
		}
		fmt.Printf("send ok         a test message was delivered to chat %s\n", *sendTo)
	}

	fmt.Println("\nThe token works and the Bot API answered. What this does NOT prove:")
	fmt.Println("  - that a person can link an account (needs the Part 2 flow in the README)")
	fmt.Println("  - that a real reply survives the HTML parser (needs one real conversation)")
	fmt.Println("Record the outcome in docs/gateways.md either way.")
	return nil
}

// reportDeliveryMode compares what North is configured to do with what Telegram
// is actually set up to do.
//
// This is the check worth having. The two modes are mutually exclusive at
// Telegram's end — while a webhook is registered, getUpdates is refused outright
// — so a leftover webhook makes polling look broken for a reason that nothing in
// the polling path can see, and a missing webhook makes production look dead
// while every test still passes.
func reportDeliveryMode(cfg config.TelegramConfig, hook telegram.WebhookInfo) {
	switch {
	case cfg.UsesWebhook() && hook.Set():
		fmt.Printf("delivery ok     webhook mode, Telegram posts to %s\n", hook.URL)

	case cfg.UsesWebhook() && !hook.Set():
		fmt.Println("PROBLEM         TELEGRAM_WEBHOOK_SECRET is set, so North serves a webhook and does not poll,")
		fmt.Println("                but Telegram has no webhook registered. Nothing will ever arrive.")
		fmt.Println("                Fix: call setWebhook (see 'Setting up Telegram' in the README).")

	case !cfg.UsesWebhook() && hook.Set():
		fmt.Printf("PROBLEM         Telegram still has a webhook registered (%s), but North is in polling mode.\n", hook.URL)
		fmt.Println("                Telegram refuses getUpdates while a webhook exists, so every poll fails.")
		fmt.Println("                Fix: call deleteWebhook, or set TELEGRAM_WEBHOOK_SECRET to run in webhook mode.")

	default:
		fmt.Println("delivery ok     polling mode, and Telegram has no webhook registered")
	}

	// Independent of the mode: these are Telegram telling you deliveries are
	// failing, and they are the fastest answer to "why is nothing arriving".
	if hook.PendingUpdateCount > 0 {
		fmt.Printf("                %d update(s) queued at Telegram\n", hook.PendingUpdateCount)
	}
	if hook.LastErrorMessage != "" {
		when := "unknown time"
		if hook.LastErrorDate > 0 {
			when = time.Unix(hook.LastErrorDate, 0).Format(time.RFC3339)
		}
		fmt.Printf("                last delivery error (%s): %s\n", when, hook.LastErrorMessage)
	}
}
