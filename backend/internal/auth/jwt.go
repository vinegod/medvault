package auth

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

const bcryptCost = 12

// Claims is embedded in every JWT we issue.
type Claims struct {
	UserID   string `json:"user_id"`
	Username string `json:"username,omitempty"` // omitted in refresh tokens
	jwt.RegisteredClaims
}

// GenerateAccessToken issues a short-lived token (15 min) carrying user_id + username.
func GenerateAccessToken(secret, userID, username string, ttl time.Duration) (string, error) {
	claims := Claims{
		UserID:   userID,
		Username: username,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   userID,
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(ttl)),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)

	signed, err := token.SignedString([]byte(secret))
	if err != nil {
		return "", fmt.Errorf("sign access token: %w", err)
	}

	return signed, nil
}

// GenerateRefreshToken issues a long-lived token (7 days) carrying only user_id.
func GenerateRefreshToken(secret, userID string, ttl time.Duration) (string, error) {
	claims := Claims{
		UserID: userID,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   userID,
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(ttl)),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)

	signed, err := token.SignedString([]byte(secret))
	if err != nil {
		return "", fmt.Errorf("sign refresh token: %w", err)
	}

	return signed, nil
}

// ValidateToken parses and verifies a JWT, returning its claims on success.
func ValidateToken(secret, tokenStr string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenStr, &Claims{}, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("unexpected signing method")
		}
		return []byte(secret), nil
	})
	if err != nil {
		return nil, fmt.Errorf("parse token: %w", err)
	}

	claims, ok := token.Claims.(*Claims)
	if !ok {
		return nil, errors.New("invalid token claims")
	}

	return claims, nil
}

// HashPassword returns a bcrypt hash of plain.
func HashPassword(plain string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(plain), bcryptCost)
	if err != nil {
		return "", fmt.Errorf("hash password: %w", err)
	}

	return string(hash), nil
}

// CheckPassword reports whether plain matches the stored bcrypt hash.
func CheckPassword(plain, hash string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(plain)) == nil
}

// dummyHash is a bcrypt hash of an arbitrary string, generated once at
// startup. Login uses it to keep response time uniform when the queried
// username doesn't exist — without this the bcrypt-skip path is ~250 ms
// faster than the bcrypt-runs path, which leaks "this username exists".
var dummyHash string

func init() {
	h, err := bcrypt.GenerateFromPassword([]byte("medvault-timing-dummy"), bcryptCost)
	if err != nil {
		// Should never happen — bcrypt only fails on cost out of range.
		panic("init dummy bcrypt hash: " + err.Error())
	}
	dummyHash = string(h)
}

// EqualizeTimingForMissingUser runs a bcrypt comparison against an internal
// dummy hash. Call this from the login handler when the user lookup returns
// no rows so the response time matches the "user exists, wrong password"
// path. Discards the result intentionally.
func EqualizeTimingForMissingUser(suppliedPassword string) {
	_ = bcrypt.CompareHashAndPassword([]byte(dummyHash), []byte(suppliedPassword))
}

// HashToken returns a hex-encoded SHA-256 digest used to store refresh tokens
// in the DB without keeping the raw token value.
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
