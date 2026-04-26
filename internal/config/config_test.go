package config

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setEnv replaces the full env for one test with a clean fixture + whatever
// overrides, then restores. Using os.Clearenv means we don't accidentally
// inherit the developer's shell state.
func setEnv(t *testing.T, overrides map[string]string) {
	t.Helper()
	orig := os.Environ()
	os.Clearenv()
	for k, v := range overrides {
		t.Setenv(k, v)
	}
	t.Cleanup(func() {
		os.Clearenv()
		for _, kv := range orig {
			for i := 0; i < len(kv); i++ {
				if kv[i] == '=' {
					_ = os.Setenv(kv[:i], kv[i+1:])
					break
				}
			}
		}
	})
}

func validEnv() map[string]string {
	return map[string]string{
		"DATABASE_URL":          "postgres://u:p@h/db",
		"DISCORD_CLIENT_ID":     "cid",
		"DISCORD_CLIENT_SECRET": "csecret",
		"DISCORD_REDIRECT_URL":  "http://localhost/cb",
		"DISCORD_BOT_TOKEN":     "bot",
		"DISCORD_ADMIN_ROLE_ID": "role",
		"DISCORD_GUILD_ID":      "guild",
		"SESSION_SECRET":        "sesh",
		"CSRF_AUTH_KEY":         "csrf",
	}
}

func TestLoad_Defaults(t *testing.T) {
	setEnv(t, validEnv())

	cfg, err := Load()
	require.NoError(t, err)

	assert.Equal(t, "8080", cfg.Port, "PORT defaults to 8080")
	assert.Equal(t, "localhost", cfg.Host, "HOST defaults to localhost")
	assert.Equal(t, 64, cfg.MaxParticipants, "MAX_PARTICIPANTS defaults to 64")
	assert.False(t, cfg.SecureCookies, "SECURE_COOKIES defaults to false")
	assert.False(t, cfg.DevLogin, "DEV_LOGIN defaults to false")
}

func TestLoad_OptionalOverrides(t *testing.T) {
	env := validEnv()
	env["PORT"] = "9090"
	env["HOST"] = "0.0.0.0"
	env["MAX_PARTICIPANTS"] = "32"
	env["SECURE_COOKIES"] = "true"
	env["DEV_LOGIN"] = "1"
	setEnv(t, env)

	cfg, err := Load()
	require.NoError(t, err)
	assert.Equal(t, "9090", cfg.Port)
	assert.Equal(t, "0.0.0.0", cfg.Host)
	assert.Equal(t, 32, cfg.MaxParticipants)
	assert.True(t, cfg.SecureCookies)
	assert.True(t, cfg.DevLogin)
}

func TestLoad_MaxParticipantsClampedAt256(t *testing.T) {
	env := validEnv()
	env["MAX_PARTICIPANTS"] = "9999"
	setEnv(t, env)

	cfg, err := Load()
	require.NoError(t, err)
	assert.Equal(t, 256, cfg.MaxParticipants, "clamped to the upper bound")
}

func TestLoad_MissingRequired(t *testing.T) {
	required := []string{
		"DATABASE_URL",
		"DISCORD_CLIENT_ID",
		"DISCORD_CLIENT_SECRET",
		"DISCORD_REDIRECT_URL",
		"DISCORD_BOT_TOKEN",
		"DISCORD_ADMIN_ROLE_ID",
		"DISCORD_GUILD_ID",
		"SESSION_SECRET",
		"CSRF_AUTH_KEY",
	}
	for _, key := range required {
		key := key
		t.Run("missing_"+key, func(t *testing.T) {
			env := validEnv()
			delete(env, key)
			setEnv(t, env)

			_, err := Load()
			require.Error(t, err)
			assert.Contains(t, err.Error(), key)
		})
	}
}

func TestLoad_BoolParsing(t *testing.T) {
	for _, v := range []string{"true", "1", "yes"} {
		env := validEnv()
		env["DEV_LOGIN"] = v
		setEnv(t, env)
		cfg, err := Load()
		require.NoError(t, err)
		assert.True(t, cfg.DevLogin, "value %q parses as true", v)
	}
	for _, v := range []string{"false", "0", "no", "bogus"} {
		env := validEnv()
		env["DEV_LOGIN"] = v
		setEnv(t, env)
		cfg, err := Load()
		require.NoError(t, err)
		assert.False(t, cfg.DevLogin, "value %q parses as false", v)
	}
}

func TestLoad_IntParsingFallsBackOnBadInput(t *testing.T) {
	env := validEnv()
	env["MAX_PARTICIPANTS"] = "not-a-number"
	setEnv(t, env)

	cfg, err := Load()
	require.NoError(t, err)
	assert.Equal(t, 64, cfg.MaxParticipants, "falls back to default")
}
