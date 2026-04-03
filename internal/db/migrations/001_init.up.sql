-- 001_init.up.sql

CREATE TABLE users (
    id BIGSERIAL PRIMARY KEY,
    discord_id TEXT NOT NULL UNIQUE,
    username TEXT NOT NULL,
    discriminator TEXT NOT NULL DEFAULT '',
    avatar TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE tournaments (
    id BIGSERIAL PRIMARY KEY,
    name TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    format TEXT NOT NULL CHECK (format IN ('single_elimination', 'double_elimination')),
    team_size INT NOT NULL DEFAULT 1,
    max_participants INT NOT NULL DEFAULT 64,
    status TEXT NOT NULL DEFAULT 'registration' CHECK (status IN ('registration', 'active', 'completed', 'cancelled')),
    created_by BIGINT NOT NULL REFERENCES users(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE teams (
    id BIGSERIAL PRIMARY KEY,
    tournament_id BIGINT NOT NULL REFERENCES tournaments(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    captain_id BIGINT NOT NULL REFERENCES users(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(tournament_id, name)
);

CREATE TABLE team_members (
    id BIGSERIAL PRIMARY KEY,
    team_id BIGINT NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    joined_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(team_id, user_id)
);

CREATE TABLE participants (
    id BIGSERIAL PRIMARY KEY,
    tournament_id BIGINT NOT NULL REFERENCES tournaments(id) ON DELETE CASCADE,
    user_id BIGINT REFERENCES users(id),
    team_id BIGINT REFERENCES teams(id),
    seed INT,
    registered_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK (
        (user_id IS NOT NULL AND team_id IS NULL) OR
        (user_id IS NULL AND team_id IS NOT NULL)
    ),
    UNIQUE(tournament_id, user_id),
    UNIQUE(tournament_id, team_id)
);

CREATE TABLE brackets (
    id BIGSERIAL PRIMARY KEY,
    tournament_id BIGINT NOT NULL UNIQUE REFERENCES tournaments(id) ON DELETE CASCADE,
    generated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TYPE match_status AS ENUM (
    'pending',
    'ready',
    'in_progress',
    'completed',
    'bye',
    'pending_reset'
);

CREATE TYPE bracket_side AS ENUM (
    'winners',
    'losers',
    'grand_final',
    'reset'
);

CREATE TABLE matches (
    id BIGSERIAL PRIMARY KEY,
    bracket_id BIGINT NOT NULL REFERENCES brackets(id) ON DELETE CASCADE,
    round INT NOT NULL,
    match_number INT NOT NULL,
    bracket_side bracket_side NOT NULL DEFAULT 'winners',
    participant_a_id BIGINT REFERENCES participants(id),
    participant_b_id BIGINT REFERENCES participants(id),
    winner_id BIGINT REFERENCES participants(id),
    loser_id BIGINT REFERENCES participants(id),
    score_a INT,
    score_b INT,
    score_display TEXT,
    status match_status NOT NULL DEFAULT 'pending',
    next_match_id BIGINT REFERENCES matches(id),
    loser_next_match_id BIGINT REFERENCES matches(id),
    played_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(bracket_id, bracket_side, round, match_number)
);

CREATE INDEX idx_matches_bracket_id ON matches(bracket_id);
CREATE INDEX idx_matches_status ON matches(status);
CREATE INDEX idx_participants_tournament_id ON participants(tournament_id);
CREATE INDEX idx_participants_user_id ON participants(user_id);
CREATE INDEX idx_participants_team_id ON participants(team_id);
