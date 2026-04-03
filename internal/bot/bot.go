package bot

import (
	"context"
	"fmt"
	"log"

	"github.com/bwmarrin/discordgo"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/th0rn0/thournament/internal/config"
	"github.com/th0rn0/thournament/internal/tournament"
)

// Bot wraps the discordgo session and tournament dependencies.
type Bot struct {
	session     *discordgo.Session
	pool        *pgxpool.Pool
	cfg         *config.Config
	broadcaster tournament.Broadcaster
}

func New(cfg *config.Config, pool *pgxpool.Pool, broadcaster tournament.Broadcaster) (*Bot, error) {
	s, err := discordgo.New("Bot " + cfg.DiscordBotToken)
	if err != nil {
		return nil, fmt.Errorf("create discordgo session: %w", err)
	}

	b := &Bot{
		session:     s,
		pool:        pool,
		cfg:         cfg,
		broadcaster: broadcaster,
	}

	s.AddHandler(b.handleInteraction)
	return b, nil
}

func (b *Bot) Start(ctx context.Context) error {
	if err := b.session.Open(); err != nil {
		return fmt.Errorf("open discord session: %w", err)
	}

	if err := b.registerCommands(); err != nil {
		// Non-fatal: log and continue. Commands may already be registered.
		log.Printf("warn: register commands: %v", err)
	}

	log.Println("Discord bot started")

	<-ctx.Done()
	return b.stop()
}

func (b *Bot) stop() error {
	if err := b.session.Close(); err != nil {
		return fmt.Errorf("close discord session: %w", err)
	}
	return nil
}

func (b *Bot) handleInteraction(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if i.Type != discordgo.InteractionApplicationCommand {
		return
	}

	data := i.ApplicationCommandData()
	switch data.Name {
	case "tournament":
		b.handleTournamentCommand(s, i)
	case "match":
		b.handleMatchCommand(s, i)
	case "admin":
		b.handleAdminCommand(s, i)
	}
}
