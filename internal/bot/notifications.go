package bot

import (
	"fmt"

	"github.com/bwmarrin/discordgo"
)

// NotifyBracketGenerated posts a public message when a bracket is generated.
func NotifyBracketGenerated(s *discordgo.Session, guildID string, tournamentID int64, webBase string) {
	channelID := findGeneralChannel(s, guildID)
	if channelID == "" {
		return
	}
	_, _ = s.ChannelMessageSend(channelID, fmt.Sprintf(
		"🏆 **Bracket Generated!** Tournament #%d is now live.\nView the bracket: %s/tournaments/%d",
		tournamentID, webBase, tournamentID,
	))
}

// NotifyMatchReady posts when a match is ready to be played.
func NotifyMatchReady(s *discordgo.Session, guildID string, matchID, tournamentID int64, playerA, playerB string, webBase string) {
	channelID := findGeneralChannel(s, guildID)
	if channelID == "" {
		return
	}
	_, _ = s.ChannelMessageSend(channelID, fmt.Sprintf(
		"⚔️ **Match Ready!** %s vs %s (Match #%d)\nView: %s/tournaments/%d",
		playerA, playerB, matchID, webBase, tournamentID,
	))
}

// NotifyTournamentComplete posts when a tournament ends.
func NotifyTournamentComplete(s *discordgo.Session, guildID string, tournamentID int64, winnerName, webBase string) {
	channelID := findGeneralChannel(s, guildID)
	if channelID == "" {
		return
	}
	_, _ = s.ChannelMessageSend(channelID, fmt.Sprintf(
		"🎉 **Tournament #%d Complete!** Winner: **%s**\nFull results: %s/tournaments/%d",
		tournamentID, winnerName, webBase, tournamentID,
	))
}

// findGeneralChannel returns the first text channel named "general" or the first text channel in the guild.
func findGeneralChannel(s *discordgo.Session, guildID string) string {
	channels, err := s.GuildChannels(guildID)
	if err != nil {
		return ""
	}

	var firstText string
	for _, ch := range channels {
		if ch.Type != discordgo.ChannelTypeGuildText {
			continue
		}
		if firstText == "" {
			firstText = ch.ID
		}
		if ch.Name == "general" {
			return ch.ID
		}
	}
	return firstText
}
