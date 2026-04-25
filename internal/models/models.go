package models

import (
	"net/url"
	"time"
)

// AvatarURLFor returns a Discord CDN URL when avatar is set, or a UI Avatars
// placeholder generated from the name when it is empty (dev seeded users).
func AvatarURLFor(discordID, avatar, name string) string {
	if avatar != "" && discordID != "" {
		return "https://cdn.discordapp.com/avatars/" + discordID + "/" + avatar + ".png?size=64"
	}
	initials := name
	if len(initials) > 2 {
		initials = initials[:2]
	}
	return "https://ui-avatars.com/api/?name=" + url.QueryEscape(initials) + "&size=64&background=2d2d2d&color=fdb828&bold=true&format=png"
}

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
	FormatRoundRobin TournamentFormat = "round_robin"
)

type TournamentStatus string

const (
	StatusRegistration TournamentStatus = "registration"
	StatusActive       TournamentStatus = "active"
	StatusCompleted    TournamentStatus = "completed"
	StatusCancelled    TournamentStatus = "cancelled"
)

type TournamentTeamMode string

const (
	TeamModeAdminAssigned TournamentTeamMode = "admin_assigned"
	TeamModePlayerCreated TournamentTeamMode = "player_created"
)

type Tournament struct {
	ID              int64              `db:"id"`
	Name            string             `db:"name"`
	Game            string             `db:"game"`
	Description     string             `db:"description"`
	Format          TournamentFormat   `db:"format"`
	TeamMode        TournamentTeamMode `db:"team_mode"`
	TeamSize        int                `db:"team_size"`
	MaxParticipants int                `db:"max_participants"`
	Status          TournamentStatus   `db:"status"`
	CreatedBy       int64              `db:"created_by"`
	CreatedAt       time.Time          `db:"created_at"`
	UpdatedAt       time.Time          `db:"updated_at"`
}

func (t Tournament) IsTeam() bool {
	return t.TeamSize > 1
}

type Team struct {
	ID           int64     `db:"id"`
	TournamentID int64     `db:"tournament_id"`
	Name         string    `db:"name"`
	CaptainID    int64     `db:"captain_id"`
	Open         bool      `db:"open"`
	JoinPassword string    `db:"join_password"`
	InviteToken  string    `db:"invite_token"`
	CreatedAt    time.Time `db:"created_at"`
	UpdatedAt    time.Time `db:"updated_at"`

	// Joined fields
	CaptainName string   `db:"captain_name"`
	MemberCount int      `db:"member_count"`
	Members     []string `db:"-"`
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

func (p *Participant) AvatarURL() string {
	return AvatarURLFor(p.UserDiscordID, p.UserAvatar, p.DisplayName())
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

	// Joined participant display names and avatar URLs
	ParticipantAName      string `db:"participant_a_name"`
	ParticipantBName      string `db:"participant_b_name"`
	WinnerName            string `db:"winner_name"`
	ParticipantAAvatarURL string `db:"-"`
	ParticipantBAvatarURL string `db:"-"`

	// Editable is a computed field, not stored. True for completed winners/
	// losers-bracket matches whose downstream matches are all still
	// un-completed, so the result can be re-submitted in place. Populated by
	// handlers.groupBracket before rendering.
	Editable bool `db:"-"`
}
