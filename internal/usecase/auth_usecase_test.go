package usecase

import (
	"context"
	"errors"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"peelen-ms-be/internal/domain/entity"
	"peelen-ms-be/internal/domain/repository"
)

type authUserRepoFake struct {
	repository.UserRepository
	user        *entity.User
	failedCount int
}

func newAuthUserRepoFake(t *testing.T) *authUserRepoFake {
	t.Helper()
	passwordHash, err := bcrypt.GenerateFromPassword([]byte("Password123"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	return &authUserRepoFake{user: &entity.User{
		ID:                 "10000000-0000-4000-8000-000000000001",
		Username:           "worker_1",
		PasswordHash:       string(passwordHash),
		IsActive:           true,
		MustChangePassword: true,
		Roles:              []string{"WORKER"},
		Vendors:            []string{"20000000-0000-4000-8000-000000000001"},
	}}
}

func (r *authUserRepoFake) GetByUsername(_ context.Context, username string) (*entity.User, error) {
	if r.user.Username != username {
		return nil, nil
	}
	copy := *r.user
	return &copy, nil
}

func (r *authUserRepoFake) GetByID(_ context.Context, id string) (*entity.User, error) {
	if r.user.ID != id {
		return nil, nil
	}
	copy := *r.user
	return &copy, nil
}

func (r *authUserRepoFake) RecordFailedLogin(_ context.Context, _, _ string) error {
	r.failedCount++
	if r.failedCount >= 5 {
		lockedUntil := time.Now().Add(15 * time.Minute)
		r.user.LockedUntil = &lockedUntil
	}
	return nil
}

func (r *authUserRepoFake) RecordSuccessfulLogin(_ context.Context, _, _ string) error {
	r.failedCount = 0
	r.user.LockedUntil = nil
	return nil
}

func (r *authUserRepoFake) LoadAuthorization(_ context.Context, user *entity.User) error {
	user.Roles = append([]string(nil), r.user.Roles...)
	user.Sites = append([]string(nil), r.user.Sites...)
	user.Vendors = append([]string(nil), r.user.Vendors...)
	return nil
}

func (r *authUserRepoFake) ChangePassword(_ context.Context, _, passwordHash, _ string) error {
	r.user.PasswordHash = passwordHash
	r.user.MustChangePassword = false
	return nil
}

type authSessionRepoFake struct {
	repository.SessionRepository
	sessions       map[string]*entity.Session
	refresh        map[string]*entity.RefreshToken
	revokeAllCount int
}

func newAuthSessionRepoFake() *authSessionRepoFake {
	return &authSessionRepoFake{sessions: map[string]*entity.Session{}, refresh: map[string]*entity.RefreshToken{}}
}

func (r *authSessionRepoFake) CreateSessionWithRefresh(_ context.Context, session *entity.Session, refresh *entity.RefreshToken) error {
	r.sessions[session.TokenHash] = session
	r.refresh[refresh.TokenHash] = refresh
	return nil
}

func (r *authSessionRepoFake) GetByTokenHash(_ context.Context, tokenHash string) (*entity.Session, error) {
	return r.sessions[tokenHash], nil
}

func (r *authSessionRepoFake) GetRefreshTokenByHash(_ context.Context, tokenHash string) (*entity.RefreshToken, error) {
	return r.refresh[tokenHash], nil
}

func (r *authSessionRepoFake) Touch(context.Context, string) error {
	return nil
}

func (r *authSessionRepoFake) RotateRefreshToken(_ context.Context, oldHash string, replacement *entity.RefreshToken) (string, bool, error) {
	old := r.refresh[oldHash]
	if old == nil {
		return "", false, nil
	}
	if old.RevokedAt != nil {
		return old.UserID, true, nil
	}
	now := time.Now()
	old.RevokedAt = &now
	if oldSession := r.sessions[hashToken("unused")]; oldSession != nil {
		oldSession.RevokedAt = &now
	}
	r.sessions[replacement.AccessTokenHash] = &entity.Session{
		ID: replacement.SessionID, UserID: old.UserID, TokenHash: replacement.AccessTokenHash,
		ExpiresAt: replacement.AccessExpiresAt,
	}
	replacement.UserID = old.UserID
	r.refresh[replacement.TokenHash] = replacement
	return old.UserID, false, nil
}

func (r *authSessionRepoFake) RevokeAllForUser(context.Context, string, string) error {
	r.revokeAllCount++
	return nil
}

func (r *authSessionRepoFake) RevokeRefreshToken(_ context.Context, tokenHash, _ string) error {
	token := r.refresh[tokenHash]
	if token == nil || token.RevokedAt != nil {
		return errors.New("refresh token inactive")
	}
	now := time.Now()
	token.RevokedAt = &now
	return nil
}

func (r *authSessionRepoFake) RevokeByTokenHash(_ context.Context, tokenHash, _ string) error {
	session := r.sessions[tokenHash]
	if session == nil {
		return ErrInvalidToken
	}
	now := time.Now()
	session.RevokedAt = &now
	return nil
}

func TestLoginIssuesSignedAccessAndHashedRefreshTokens(t *testing.T) {
	users := newAuthUserRepoFake(t)
	sessions := newAuthSessionRepoFake()
	service := newTestAuthUsecase(users, sessions)
	result, err := service.Login(context.Background(), " Worker_1 ", "Password123", RequestMetadata{})
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if len(result.RefreshToken) == 0 || len(result.AccessToken) == 0 {
		t.Fatal("login did not return both tokens")
	}
	if sessions.sessions[hashToken(result.AccessToken)] == nil {
		t.Fatal("access token hash was not persisted")
	}
	if sessions.refresh[hashToken(result.RefreshToken)] == nil {
		t.Fatal("refresh token hash was not persisted")
	}
	if sessions.refresh[result.RefreshToken] != nil {
		t.Fatal("raw refresh token was persisted")
	}
	if _, err := service.ValidateToken(context.Background(), result.AccessToken); err != nil {
		t.Fatalf("validate issued access token: %v", err)
	}
}

func TestRefreshRotatesTokenAndDetectsReuse(t *testing.T) {
	users := newAuthUserRepoFake(t)
	sessions := newAuthSessionRepoFake()
	service := newTestAuthUsecase(users, sessions)
	login, err := service.Login(context.Background(), "worker_1", "Password123", RequestMetadata{})
	if err != nil {
		t.Fatal(err)
	}
	refreshed, err := service.Refresh(context.Background(), login.RefreshToken, RequestMetadata{})
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if refreshed.RefreshToken == login.RefreshToken {
		t.Fatal("refresh token was not rotated")
	}
	if _, err := service.Refresh(context.Background(), login.RefreshToken, RequestMetadata{}); !errors.Is(err, ErrRefreshReuse) {
		t.Fatalf("reused token error = %v, want ErrRefreshReuse", err)
	}
	if sessions.revokeAllCount != 1 {
		t.Fatalf("reuse should revoke all user sessions; calls = %d", sessions.revokeAllCount)
	}
}

func TestFiveFailedPasswordsLockAccount(t *testing.T) {
	users := newAuthUserRepoFake(t)
	users.user.Roles = []string{"ADMIN"} // WORKER role is exempt from lockout
	sessions := newAuthSessionRepoFake()
	service := newTestAuthUsecase(users, sessions)
	for attempt := 1; attempt <= 5; attempt++ {
		_, err := service.Login(context.Background(), "worker_1", "wrong-password1", RequestMetadata{})
		if attempt < 5 && !errors.Is(err, ErrInvalidCredentials) {
			t.Fatalf("attempt %d error = %v, want invalid credentials", attempt, err)
		}
		if attempt == 5 && !errors.Is(err, ErrAccountLocked) {
			t.Fatalf("fifth attempt error = %v, want account locked", err)
		}
	}
}

func newTestAuthUsecase(users repository.UserRepository, sessions repository.SessionRepository) AuthUsecase {
	return NewAuthUsecase(users, sessions, AuthUsecaseConfig{
		AccessTokenDuration: time.Minute, RefreshTokenDuration: time.Hour, BcryptCost: 12,
		ActiveKeyID: "test", JWTKeys: map[string][]byte{"test": []byte("01234567890123456789012345678901")},
	})
}
