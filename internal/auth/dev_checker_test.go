package auth_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/th0rn0/lanops-tournament-manager/internal/auth"
)

// stubChecker lets tests control what the inner checker returns without
// hitting Discord.
type stubChecker struct {
	calledFor string
	ret       bool
	err       error
}

func (s *stubChecker) IsAdmin(_ context.Context, id string) (bool, error) {
	s.calledFor = id
	return s.ret, s.err
}

func TestDevAdminChecker_ShortCircuitsDevAdmin(t *testing.T) {
	inner := &stubChecker{ret: false, err: errors.New("would be called")}
	d := &auth.DevAdminChecker{Inner: inner}

	ok, err := d.IsAdmin(context.Background(), auth.DevPrefixAdmin+"th0rn0")
	assert.NoError(t, err)
	assert.True(t, ok)
	assert.Empty(t, inner.calledFor, "dev-admin-* must not delegate to Discord")
}

func TestDevAdminChecker_ShortCircuitsDevUser(t *testing.T) {
	inner := &stubChecker{ret: true, err: errors.New("would be called")}
	d := &auth.DevAdminChecker{Inner: inner}

	ok, err := d.IsAdmin(context.Background(), auth.DevPrefixUser+"regular")
	assert.NoError(t, err)
	assert.False(t, ok, "dev-user-* is never admin even if inner would say yes")
	assert.Empty(t, inner.calledFor, "must not delegate to Discord")
}

func TestDevAdminChecker_DelegatesRealIDs(t *testing.T) {
	inner := &stubChecker{ret: true}
	d := &auth.DevAdminChecker{Inner: inner}

	ok, err := d.IsAdmin(context.Background(), "discord_user_12345")
	assert.NoError(t, err)
	assert.True(t, ok, "non-dev IDs pass through to the real checker")
	assert.Equal(t, "discord_user_12345", inner.calledFor)
}

func TestDevAdminChecker_PropagatesInnerError(t *testing.T) {
	boom := errors.New("discord unreachable")
	inner := &stubChecker{ret: false, err: boom}
	d := &auth.DevAdminChecker{Inner: inner}

	_, err := d.IsAdmin(context.Background(), "discord_user_99")
	assert.ErrorIs(t, err, boom)
}
