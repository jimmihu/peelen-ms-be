package usecase

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strings"
	"time"
	"unicode"

	"golang.org/x/crypto/bcrypt"

	"peelen-ms-be/internal/domain/entity"
	"peelen-ms-be/internal/domain/repository"
)

var (
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrAccountLocked      = errors.New("account locked")
	ErrUserNotFound       = errors.New("user not found")
	ErrInvalidToken       = errors.New("invalid or expired token")
	ErrRefreshReuse       = errors.New("revoked refresh token reuse detected")
	ErrPasswordPolicy     = errors.New("password must contain at least eight characters, a letter, and a number")
	ErrUsernamePolicy     = errors.New("username must be 3-30 lowercase letters, digits, dots, or underscores")
)

type RequestMetadata struct {
	IPAddress string
	UserAgent string
}

type AuthUsecase interface {
	Login(ctx context.Context, username, password string, metadata RequestMetadata) (*LoginResponse, error)
	Refresh(ctx context.Context, refreshToken string, metadata RequestMetadata) (*LoginResponse, error)
	Logout(ctx context.Context, userID, accessToken, refreshToken, ipAddress string) error
	LogoutAll(ctx context.Context, userID, ipAddress string) error
	ChangePassword(ctx context.Context, userID, currentPassword, newPassword, ipAddress string) error
	ValidateToken(ctx context.Context, token string) (*entity.User, error)
}

type LoginResponse struct {
	User             *entity.User `json:"user"`
	AccessToken      string       `json:"access_token"`
	RefreshToken     string       `json:"refresh_token"`
	ExpiresAt        time.Time    `json:"expires_at"`
	RefreshExpiresAt time.Time    `json:"-"`
}

type AuthUsecaseConfig struct {
	AccessTokenDuration  time.Duration
	RefreshTokenDuration time.Duration
	BcryptCost           int
	ActiveKeyID          string
	JWTKeys              map[string][]byte
}

type AuthUsecaseImpl struct {
	userRepo    repository.UserRepository
	sessionRepo repository.SessionRepository
	config      AuthUsecaseConfig
}

func NewAuthUsecase(
	userRepo repository.UserRepository,
	sessionRepo repository.SessionRepository,
	config AuthUsecaseConfig,
) AuthUsecase {
	return &AuthUsecaseImpl{userRepo: userRepo, sessionRepo: sessionRepo, config: config}
}

func (u *AuthUsecaseImpl) Login(ctx context.Context, username, password string, metadata RequestMetadata) (*LoginResponse, error) {
	username = strings.ToLower(strings.TrimSpace(username))
	if !validUsername(username) {
		if err := u.userRepo.RecordFailedLogin(ctx, username, metadata.IPAddress); err != nil {
			return nil, err
		}
		return nil, ErrInvalidCredentials
	}
	user, err := u.userRepo.GetByUsername(ctx, username)
	if err != nil {
		return nil, err
	}
	if user == nil || !user.IsActive {
		if err := u.userRepo.RecordFailedLogin(ctx, username, metadata.IPAddress); err != nil {
			return nil, err
		}
		return nil, ErrInvalidCredentials
	}
	// Load roles early so we can skip the locking mechanism for WORKER accounts.
	if err := u.userRepo.LoadAuthorization(ctx, user); err != nil {
		return nil, err
	}
	isWorker := hasRole(user, "WORKER")
	if !isWorker {
		if user.LockedUntil != nil && time.Now().Before(*user.LockedUntil) {
			if err := u.userRepo.RecordFailedLogin(ctx, username, metadata.IPAddress); err != nil {
				return nil, err
			}
			return nil, ErrAccountLocked
		}
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
		if err := u.userRepo.RecordFailedLogin(ctx, username, metadata.IPAddress); err != nil {
			return nil, err
		}
		if !isWorker {
			updated, loadErr := u.userRepo.GetByUsername(ctx, username)
			if loadErr != nil {
				return nil, loadErr
			}
			if updated != nil && updated.LockedUntil != nil && time.Now().Before(*updated.LockedUntil) {
				return nil, ErrAccountLocked
			}
		}
		return nil, ErrInvalidCredentials
	}
	if err := u.userRepo.RecordSuccessfulLogin(ctx, user.ID, metadata.IPAddress); err != nil {
		return nil, err
	}
	user.FailedLoginCount = 0
	user.LockedUntil = nil
	now := time.Now()
	user.LastLoginAt = &now
	// LoadAuthorization was already called above; no need to call it again.
	return u.issueTokens(ctx, user, metadata)
}

func (u *AuthUsecaseImpl) Refresh(ctx context.Context, rawRefreshToken string, metadata RequestMetadata) (*LoginResponse, error) {
	refresh, err := u.sessionRepo.GetRefreshTokenByHash(ctx, hashToken(rawRefreshToken))
	if err != nil {
		return nil, err
	}
	if refresh == nil {
		return nil, ErrInvalidToken
	}
	if refresh.RevokedAt != nil {
		if err := u.sessionRepo.RevokeAllForUser(ctx, refresh.UserID, metadata.IPAddress); err != nil {
			return nil, err
		}
		return nil, ErrRefreshReuse
	}
	if !time.Now().Before(refresh.ExpiresAt) {
		return nil, ErrInvalidToken
	}
	user, err := u.userRepo.GetByID(ctx, refresh.UserID)
	if err != nil {
		return nil, err
	}
	if user == nil || !user.IsActive {
		if err := u.sessionRepo.RevokeAllForUser(ctx, refresh.UserID, metadata.IPAddress); err != nil {
			return nil, err
		}
		return nil, ErrInvalidToken
	}
	if err := u.userRepo.LoadAuthorization(ctx, user); err != nil {
		return nil, err
	}

	sessionID, err := newUUID()
	if err != nil {
		return nil, err
	}
	newRawRefresh, err := generateToken(32)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	accessExpiry := now.Add(u.config.AccessTokenDuration)
	refreshExpiry := now.Add(u.config.RefreshTokenDuration)
	accessToken, err := signAccessToken(tokenClaims{
		Subject: user.ID, Roles: user.Roles, Expires: accessExpiry.Unix(), Issued: now.Unix(), TokenID: sessionID,
	}, u.config.ActiveKeyID, u.config.JWTKeys)
	if err != nil {
		return nil, err
	}
	replacement := &entity.RefreshToken{
		UserID: user.ID, SessionID: sessionID, TokenHash: hashToken(newRawRefresh),
		AccessTokenHash: hashToken(accessToken), ExpiresAt: refreshExpiry, AccessExpiresAt: accessExpiry,
		UserAgent: metadata.UserAgent, IPAddress: metadata.IPAddress,
	}
	rotatedUserID, reused, err := u.sessionRepo.RotateRefreshToken(ctx, hashToken(rawRefreshToken), replacement)
	if err != nil {
		return nil, err
	}
	if reused {
		return nil, ErrRefreshReuse
	}
	if rotatedUserID == "" {
		return nil, ErrInvalidToken
	}
	return &LoginResponse{
		User: user, AccessToken: accessToken, RefreshToken: newRawRefresh,
		ExpiresAt: accessExpiry, RefreshExpiresAt: refreshExpiry,
	}, nil
}

func (u *AuthUsecaseImpl) issueTokens(ctx context.Context, user *entity.User, metadata RequestMetadata) (*LoginResponse, error) {
	now := time.Now()
	sessionID, err := newUUID()
	if err != nil {
		return nil, err
	}
	rawAccess, err := signAccessToken(tokenClaims{
		Subject: user.ID, Roles: user.Roles, Expires: now.Add(u.config.AccessTokenDuration).Unix(),
		Issued: now.Unix(), TokenID: sessionID,
	}, u.config.ActiveKeyID, u.config.JWTKeys)
	if err != nil {
		return nil, err
	}
	rawRefresh, err := generateToken(32)
	if err != nil {
		return nil, err
	}
	session := &entity.Session{
		ID: sessionID, UserID: user.ID, TokenHash: hashToken(rawAccess),
		ExpiresAt: now.Add(u.config.AccessTokenDuration), UserAgent: metadata.UserAgent, IPAddress: metadata.IPAddress,
	}
	refresh := &entity.RefreshToken{
		UserID: user.ID, SessionID: sessionID, TokenHash: hashToken(rawRefresh),
		ExpiresAt: now.Add(u.config.RefreshTokenDuration), UserAgent: metadata.UserAgent, IPAddress: metadata.IPAddress,
	}
	if err := u.sessionRepo.CreateSessionWithRefresh(ctx, session, refresh); err != nil {
		return nil, err
	}
	return &LoginResponse{
		User: user, AccessToken: rawAccess, RefreshToken: rawRefresh,
		ExpiresAt: session.ExpiresAt, RefreshExpiresAt: refresh.ExpiresAt,
	}, nil
}

func (u *AuthUsecaseImpl) Logout(ctx context.Context, userID, accessToken, refreshToken, ipAddress string) error {
	if refreshToken != "" {
		refresh, err := u.sessionRepo.GetRefreshTokenByHash(ctx, hashToken(refreshToken))
		if err != nil {
			return err
		}
		if refresh == nil || refresh.UserID != userID {
			return ErrInvalidToken
		}
		if refresh.RevokedAt != nil {
			return ErrInvalidToken
		}
		return u.sessionRepo.RevokeRefreshToken(ctx, hashToken(refreshToken), ipAddress)
	}
	return u.sessionRepo.RevokeByTokenHash(ctx, hashToken(accessToken), ipAddress)
}

func (u *AuthUsecaseImpl) LogoutAll(ctx context.Context, userID, ipAddress string) error {
	return u.sessionRepo.RevokeAllForUser(ctx, userID, ipAddress)
}

func (u *AuthUsecaseImpl) ChangePassword(ctx context.Context, userID, currentPassword, newPassword, ipAddress string) error {
	if err := validatePassword(newPassword); err != nil {
		return err
	}
	user, err := u.userRepo.GetByID(ctx, userID)
	if err != nil {
		return err
	}
	if user == nil || bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(currentPassword)) != nil {
		return ErrInvalidCredentials
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), u.config.BcryptCost)
	if err != nil {
		return err
	}
	return u.userRepo.ChangePassword(ctx, userID, string(hash), ipAddress)
}

func (u *AuthUsecaseImpl) ValidateToken(ctx context.Context, token string) (*entity.User, error) {
	claims, err := verifyAccessToken(token, u.config.JWTKeys, time.Now())
	if err != nil {
		return nil, ErrInvalidToken
	}
	session, err := u.sessionRepo.GetByTokenHash(ctx, hashToken(token))
	if err != nil {
		return nil, err
	}
	if session == nil || session.ID != claims.TokenID || session.UserID != claims.Subject || !session.IsValid() {
		return nil, ErrInvalidToken
	}
	user, err := u.userRepo.GetByID(ctx, claims.Subject)
	if err != nil {
		return nil, err
	}
	if user == nil || !user.IsActive {
		return nil, ErrInvalidToken
	}
	if err := u.userRepo.LoadAuthorization(ctx, user); err != nil {
		return nil, err
	}
	if !sameStrings(claims.Roles, user.Roles) {
		return nil, ErrInvalidToken
	}
	if err := u.sessionRepo.Touch(ctx, hashToken(token)); err != nil {
		return nil, err
	}
	return user, nil
}

func sameStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	set := make(map[string]int, len(left))
	for _, value := range left {
		set[value]++
	}
	for _, value := range right {
		set[value]--
	}
	for _, count := range set {
		if count != 0 {
			return false
		}
	}
	return true
}

func generateToken(length int) (string, error) {
	random := make([]byte, length)
	if _, err := rand.Read(random); err != nil {
		return "", err
	}
	return hex.EncodeToString(random), nil
}

func validatePassword(password string) error {
	if len(password) < 8 || len(password) > 72 {
		return ErrPasswordPolicy
	}
	hasLetter, hasNumber := false, false
	for _, r := range password {
		if unicode.IsLetter(r) {
			hasLetter = true
		}
		if unicode.IsDigit(r) {
			hasNumber = true
		}
	}
	if !hasLetter || !hasNumber {
		return ErrPasswordPolicy
	}
	return nil
}

func validUsername(username string) bool {
	if len(username) < 3 || len(username) > 30 {
		return false
	}
	for _, r := range username {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '.' && r != '_' {
			return false
		}
	}
	return true
}
