package auth

import (
	"context"
	"strings"
)

// DevPrefixAdmin and DevPrefixUser are discord_id prefixes recognised by
// DevAdminChecker. Users created via /dev/login get one of these prefixes
// so the admin check can short-circuit without hitting Discord.
const (
	DevPrefixAdmin = "dev-admin-"
	DevPrefixUser  = "dev-user-"
)

// DevAdminChecker wraps a real AdminChecker and short-circuits IDs that
// were minted by the /dev/login flow. Only wired in when DEV_LOGIN=true.
type DevAdminChecker struct {
	Inner AdminChecker
}

func (d *DevAdminChecker) IsAdmin(ctx context.Context, discordUserID string) (bool, error) {
	if strings.HasPrefix(discordUserID, DevPrefixAdmin) {
		return true, nil
	}
	if strings.HasPrefix(discordUserID, DevPrefixUser) {
		return false, nil
	}
	return d.Inner.IsAdmin(ctx, discordUserID)
}
