package handlers_test

// Auth endpoint coverage: register, login, refresh rotation, logout,
// change-password. Negative paths (duplicate username, wrong password,
// reused refresh token) are the security-load-bearing cases — those
// must keep failing the same way over time.

import (
	"net/http"
	"testing"
	"time"
)

func TestAuth_Register_Happy(t *testing.T) {
	h := newHarness(t)

	status, data, errEnv := h.do("POST", "/api/auth/register", "", map[string]string{
		"username": "alice", "password": "alicepass123",
	})
	if status != http.StatusCreated {
		t.Fatalf("status=%d err=%v", status, errEnv)
	}
	if data["access_token"] == "" || data["refresh_token"] == "" {
		t.Errorf("missing tokens: %v", data)
	}
	user, _ := data["user"].(map[string]any)
	if user["username"] != "alice" {
		t.Errorf("username = %v, want alice", user["username"])
	}
}

func TestAuth_Register_DuplicateUsername(t *testing.T) {
	h := newHarness(t)
	h.register("alice", "alicepass123")

	status, _, errEnv := h.do("POST", "/api/auth/register", "", map[string]string{
		"username": "alice", "password": "alicepass123",
	})
	if status != http.StatusConflict {
		t.Fatalf("status=%d, want 409 (err=%v)", status, errEnv)
	}
}

func TestAuth_Register_RejectsShortPassword(t *testing.T) {
	h := newHarness(t)

	status, _, _ := h.do("POST", "/api/auth/register", "", map[string]string{
		"username": "alice", "password": "short",
	})
	if status != http.StatusBadRequest {
		t.Errorf("status=%d, want 400 for 5-char password", status)
	}
}

func TestAuth_Login_Happy(t *testing.T) {
	h := newHarness(t)
	h.register("alice", "alicepass123")

	status, data, _ := h.do("POST", "/api/auth/login", "", map[string]string{
		"username": "alice", "password": "alicepass123",
	})
	if status != http.StatusOK {
		t.Fatalf("status=%d", status)
	}
	if data["access_token"] == "" {
		t.Errorf("no access_token in login response")
	}
}

func TestAuth_Login_WrongPassword(t *testing.T) {
	h := newHarness(t)
	h.register("alice", "alicepass123")

	status, _, _ := h.do("POST", "/api/auth/login", "", map[string]string{
		"username": "alice", "password": "wrong-password",
	})
	if status != http.StatusUnauthorized {
		t.Errorf("status=%d, want 401", status)
	}
}

func TestAuth_Login_UnknownUserSameError(t *testing.T) {
	h := newHarness(t)

	// Same response shape and status as wrong-password, so an attacker
	// can't tell from the HTTP layer whether the username exists.
	status, _, errEnv := h.do("POST", "/api/auth/login", "", map[string]string{
		"username": "ghost", "password": "anything-goes",
	})
	if status != http.StatusUnauthorized {
		t.Fatalf("status=%d, want 401", status)
	}
	if errEnv["code"] != "UNAUTHORIZED" {
		t.Errorf("code = %v, want UNAUTHORIZED", errEnv["code"])
	}
}

func TestAuth_Refresh_RotatesAndInvalidatesOld(t *testing.T) {
	h := newHarness(t)

	// Use register's own refresh token directly (not login's) so the DB
	// has exactly one row with this token's hash. Otherwise, register +
	// login in the same wall-clock second mint identical JWTs (second-
	// precision iat) → identical hashes → two rows → the rotation delete
	// only removes one, leaving the "old" token still valid.
	regStatus, regData, _ := h.do("POST", "/api/auth/register", "", map[string]string{
		"username": "alice", "password": "alicepass123",
	})
	if regStatus != http.StatusCreated {
		t.Fatalf("register status=%d", regStatus)
	}
	oldRefresh := regData["refresh_token"].(string)

	// JWT iat is second-precision; sleep so the next mint differs.
	time.Sleep(1100 * time.Millisecond)

	status, refreshed, errEnv := h.do("POST", "/api/auth/refresh", "", map[string]string{
		"refresh_token": oldRefresh,
	})
	if status != http.StatusOK {
		t.Fatalf("first refresh: status=%d err=%v", status, errEnv)
	}
	newRefresh, _ := refreshed["refresh_token"].(string)
	if newRefresh == "" || newRefresh == oldRefresh {
		t.Errorf("refresh token did not rotate: old=%q new=%q", oldRefresh, newRefresh)
	}

	// Reusing the old token must fail (rotation revokes it).
	status, _, _ = h.do("POST", "/api/auth/refresh", "", map[string]string{
		"refresh_token": oldRefresh,
	})
	if status != http.StatusUnauthorized {
		t.Errorf("reused old token: status=%d, want 401", status)
	}
}

func TestAuth_Logout_RevokesRefreshToken(t *testing.T) {
	h := newHarness(t)
	_, _ = h.register("alice", "alicepass123")

	_, loginData, _ := h.do("POST", "/api/auth/login", "", map[string]string{
		"username": "alice", "password": "alicepass123",
	})
	refresh := loginData["refresh_token"].(string)

	status, _, _ := h.do("POST", "/api/auth/logout", "", map[string]string{
		"refresh_token": refresh,
	})
	if status != http.StatusOK {
		t.Fatalf("logout status=%d", status)
	}

	// Refresh with the revoked token fails.
	status, _, _ = h.do("POST", "/api/auth/refresh", "", map[string]string{
		"refresh_token": refresh,
	})
	if status != http.StatusUnauthorized {
		t.Errorf("post-logout refresh: status=%d, want 401", status)
	}
}

func TestAuth_ChangePassword_HappyAndRevokes(t *testing.T) {
	h := newHarness(t)
	tok, _ := h.register("alice", "alicepass123")

	// Grab a separate refresh token via login to confirm it gets revoked.
	_, loginData, _ := h.do("POST", "/api/auth/login", "", map[string]string{
		"username": "alice", "password": "alicepass123",
	})
	otherDeviceRefresh := loginData["refresh_token"].(string)

	// Same JWT-iat-collision dodge as in the rotation test: ChangePassword
	// issues a new refresh token, and we want its hash to differ from the
	// otherDeviceRefresh hash so we're truly testing revocation, not
	// "they happened to be the same string".
	time.Sleep(1100 * time.Millisecond)

	status, newPair, errEnv := h.do("POST", "/api/auth/password", tok, map[string]string{
		"current_password": "alicepass123",
		"new_password":     "newpassword456",
	})
	if status != http.StatusOK {
		t.Fatalf("change password: status=%d err=%v", status, errEnv)
	}
	if newPair["access_token"] == "" {
		t.Errorf("response missing fresh access_token")
	}

	// The other device's refresh token is now revoked.
	status, _, _ = h.do("POST", "/api/auth/refresh", "", map[string]string{
		"refresh_token": otherDeviceRefresh,
	})
	if status != http.StatusUnauthorized {
		t.Errorf("other-device refresh after password change: status=%d, want 401", status)
	}

	// Login with new password works.
	status, _, _ = h.do("POST", "/api/auth/login", "", map[string]string{
		"username": "alice", "password": "newpassword456",
	})
	if status != http.StatusOK {
		t.Errorf("login with new password: status=%d, want 200", status)
	}

	// Login with old password fails.
	status, _, _ = h.do("POST", "/api/auth/login", "", map[string]string{
		"username": "alice", "password": "alicepass123",
	})
	if status != http.StatusUnauthorized {
		t.Errorf("login with old password: status=%d, want 401", status)
	}
}

func TestAuth_ChangePassword_WrongCurrent(t *testing.T) {
	h := newHarness(t)
	tok, _ := h.register("alice", "alicepass123")

	status, _, _ := h.do("POST", "/api/auth/password", tok, map[string]string{
		"current_password": "wrong-password",
		"new_password":     "newpassword456",
	})
	if status != http.StatusUnauthorized {
		t.Errorf("status=%d, want 401", status)
	}
}
