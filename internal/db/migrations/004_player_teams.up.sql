-- team_mode on tournaments: admin_assigned (default) or player_created
ALTER TABLE tournaments ADD COLUMN IF NOT EXISTS team_mode TEXT NOT NULL DEFAULT 'admin_assigned'
    CHECK (team_mode IN ('admin_assigned', 'player_created'));

-- teams: optional join password, open flag, invite token
ALTER TABLE teams ADD COLUMN IF NOT EXISTS open BOOLEAN NOT NULL DEFAULT true;
ALTER TABLE teams ADD COLUMN IF NOT EXISTS join_password TEXT NOT NULL DEFAULT '';
ALTER TABLE teams ADD COLUMN IF NOT EXISTS invite_token TEXT NOT NULL DEFAULT '';
