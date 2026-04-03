package models

import "time"

type User struct {
	ID            int64     `db:"id"`
	DiscordID     string    `db:"discord_id"`
	Username      string    `db:"username"`
	Discriminator string    `db:"discriminator"`
	Avatar        string    `db:"avatar"`
	CreatedAt     time.Time `db:"created_at"`
	UpdatedAt     time.Time `db:"updated_at"`
}

func (u *User) AvatarURL() string {
	if u.Avatar == "" {
		return ""
	}
	return "https://cdn.discordapp.com/avatars/" + u.DiscordID + "/" + u.Avatar + ".png"
}

type TournamentFormat string

const (
	FormatSingleElim TournamentFormat = "single_elimination"
	FormatDoubleElim TournamentFormat = "double_elimination"
)

type TournamentStatus string

const (
	StatusRegistration TournamentStatus = "registration"
	StatusActive       TournamentStatus = "active"
	StatusCompleted    TournamentStatus = "completed"
	StatusCancelled    TournamentStatus = "cancelled"
)

type Tournament struct {
	ID              int64            `db:"id"`
	Name            string           `db:"name"`
	Description     string           `db:"description"`
	Format          TournamentFormat `db:"format"`
	TeamSize        int              `db:"team_size"`
	MaxParticipants int              `db:"max_participants"`
	Status          TournamentStatus `db:"status"`
	CreatedBy       int64            `db:"created_by"`
	CreatedAt       time.Time        `db:"created_at"`
	UpdatedAt       time.Time        `db:"updated_at"`
}

func (t *Tournament) IsTeam() bool {
	return t.TeamSize > 1
}

type Team struct {
	ID           int64     `db:"id"`
	TournamentID int64     `db:"tournament_id"`
	Name         string    `db:"name"`
	CaptainID    int64     `db:"captain_id"`
	CreatedAt    time.Time `db:"created_at"`
	UpdatedAt    time.Time `db:"updated_at"`
}

type TeamMember struct {
	ID       int64     `db:"id"`
	TeamID   int64     `db:"team_id"`
	UserID   int64     `db:"user_id"`
	JoinedAt time.Time `db:"joined_at"`
}

type Participant struct {
	ID           int64     `db:"id"`
	TournamentID int64     `db:"tournament_id"`
	UserID       *int64    `db:"user_id"`
	TeamID       *int64    `db:"team_id"`
	Seed         *int      `db:"seed"`
	RegisteredAt time.Time `db:"registered_at"`

	// Joined fields
	UserUsername string `db:"user_username"`
	UserAvatar   string `db:"user_avatar"`
	UserDiscordID string `db:"user_discord_id"`
	TeamName     string `db:"team_name"`
}

func (p *Participant) DisplayName() string {
	if p.TeamName != "" {
		return p.TeamName
	}
	return p.UserUsername
}

type Bracket struct {
	ID           int64     `db:"id"`
	TournamentID int64     `db:"tournament_id"`
	GeneratedAt  time.Time `db:"generated_at"`
}

type MatchStatus string

const (
	MatchPending      MatchStatus = "pending"
	MatchReady        MatchStatus = "ready"
	MatchInProgress   MatchStatus = "in_progress"
	MatchCompleted    MatchStatus = "completed"
	MatchBye          MatchStatus = "bye"
	MatchPendingReset MatchStatus = "pending_reset"
)

type BracketSide string

const (
	SideWinners    BracketSide = "winners"
	SideLosers     BracketSide = "losers"
	SideGrandFinal BracketSide = "grand_final"
	SideReset      BracketSide = "reset"
)

type Match struct {
	ID              int64       `db:"id"`
	BracketID       int64       `db:"bracket_id"`
	Round           int         `db:"round"`
	MatchNumber     int         `db:"match_number"`
	BracketSide     BracketSide `db:"bracket_side"`
	ParticipantAID  *int64      `db:"participant_a_id"`
	ParticipantBID  *int64      `db:"participant_b_id"`
	WinnerID        *int64      `db:"winner_id"`
	LoserID         *int64      `db:"loser_id"`
	ScoreA          *int        `db:"score_a"`
	ScoreB          *int        `db:"score_b"`
	ScoreDisplay    *string     `db:"score_display"`
	Status          MatchStatus `db:"status"`
	NextMatchID     *int64      `db:"next_match_id"`
	LoserNextMatchID *int64     `db:"loser_next_match_id"`
	PlayedAt        *time.Time  `db:"played_at"`
	CreatedAt       time.Time   `db:"created_at"`
	UpdatedAt       time.Time   `db:"updated_at"`

	// Joined participant display names
	ParticipantAName string `db:"participant_a_name"`
	ParticipantBName string `db:"participant_b_name"`
	WinnerName       string `db:"winner_name"`
}
