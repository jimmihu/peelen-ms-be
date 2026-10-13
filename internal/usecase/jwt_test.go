package usecase

import (
	"strings"
	"testing"
	"time"
)

func TestAccessTokenVerificationAndRotationKeys(t *testing.T) {
	now := time.Now()
	keys := map[string][]byte{
		"old": []byte(strings.Repeat("o", 32)),
		"new": []byte(strings.Repeat("n", 32)),
	}
	token, err := signAccessToken(tokenClaims{
		Subject: "user-id", Roles: []string{"WORKER"}, Expires: now.Add(time.Minute).Unix(),
		Issued: now.Unix(), TokenID: "session-id",
	}, "new", keys)
	if err != nil {
		t.Fatal(err)
	}
	claims, err := verifyAccessToken(token, keys, now)
	if err != nil {
		t.Fatalf("verify token: %v", err)
	}
	if claims.Subject != "user-id" || claims.TokenID != "session-id" || len(claims.Roles) != 1 {
		t.Fatalf("unexpected token claims: %+v", claims)
	}

	if _, err := verifyAccessToken(token, map[string][]byte{"old": keys["old"]}, now); err == nil {
		t.Fatal("token signed by unknown key id was accepted")
	}
}

func TestAccessTokenRejectsTamperingAndExpiry(t *testing.T) {
	now := time.Now()
	keys := map[string][]byte{"current": []byte(strings.Repeat("x", 32))}
	token, err := signAccessToken(tokenClaims{
		Subject: "user-id", Roles: []string{}, Expires: now.Add(time.Second).Unix(),
		Issued: now.Unix(), TokenID: "session-id",
	}, "current", keys)
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(token, ".")
	replacement := byte('A')
	if parts[1][0] == replacement {
		replacement = 'B'
	}
	parts[1] = string(replacement) + parts[1][1:]
	tampered := strings.Join(parts, ".")
	if _, err := verifyAccessToken(tampered, keys, now); err == nil {
		t.Fatal("modified token signature was accepted")
	}
	if _, err := verifyAccessToken(token, keys, now.Add(2*time.Second)); err == nil {
		t.Fatal("expired token was accepted")
	}
}

func TestUsernameAndPasswordPolicies(t *testing.T) {
	for _, username := range []string{"worker_1", "admin.site", "abc"} {
		if !validUsername(username) {
			t.Errorf("expected valid username %q", username)
		}
	}
	for _, username := range []string{"UPPER", "user-name", "ab", "this_username_is_too_long_for_the_limit"} {
		if validUsername(username) {
			t.Errorf("expected invalid username %q", username)
		}
	}
	for _, password := range []string{"12345678", "short1"} {
		if validatePassword(password) == nil {
			t.Errorf("expected invalid password %q", password)
		}
	}
	if err := validatePassword("safe-pass-2026"); err != nil {
		t.Fatalf("expected valid password: %v", err)
	}
}
