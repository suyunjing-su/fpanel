package auth

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/argon2"
)

type Identity struct {
	UserID       int64
	Username     string
	Role         string
	RoleID       int
	TokenVersion int
}

type claims struct {
	Username     string `json:"username"`
	User         string `json:"user"`
	Name         string `json:"name"`
	Role         string `json:"role"`
	RoleID       int    `json:"role_id"`
	TokenVersion int    `json:"ver"`
	jwt.RegisteredClaims
}

type Manager struct {
	secret []byte
	ttl    time.Duration
}

func New(secret string, ttl time.Duration) *Manager {
	return &Manager{secret: []byte(secret), ttl: ttl}
}

func (m *Manager) Issue(identity Identity) (string, error) {
	now := time.Now()
	value := claims{
		Username:     identity.Username,
		User:         identity.Username,
		Name:         identity.Username,
		Role:         identity.Role,
		RoleID:       identity.RoleID,
		TokenVersion: identity.TokenVersion,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   strconv.FormatInt(identity.UserID, 10),
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(m.ttl)),
			NotBefore: jwt.NewNumericDate(now.Add(-5 * time.Second)),
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, value).SignedString(m.secret)
}

func (m *Manager) Parse(raw string) (Identity, error) {
	parsed, err := jwt.ParseWithClaims(raw, &claims{}, func(token *jwt.Token) (any, error) {
		if token.Method != jwt.SigningMethodHS256 {
			return nil, errors.New("unexpected signing method")
		}
		return m.secret, nil
	}, jwt.WithExpirationRequired(), jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}))
	if err != nil || !parsed.Valid {
		return Identity{}, errors.New("invalid token")
	}
	value, ok := parsed.Claims.(*claims)
	if !ok {
		return Identity{}, errors.New("invalid claims")
	}
	userID, err := strconv.ParseInt(value.Subject, 10, 64)
	if err != nil || userID <= 0 {
		return Identity{}, errors.New("invalid subject")
	}
	return Identity{UserID: userID, Username: value.Username, Role: value.Role, RoleID: value.RoleID, TokenVersion: value.TokenVersion}, nil
}

func HashPassword(password string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generate password salt: %w", err)
	}
	hash := argon2.IDKey([]byte(password), salt, 3, 64*1024, 2, 32)
	return fmt.Sprintf("$argon2id$v=19$m=65536,t=3,p=2$%s$%s", base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(hash)), nil
}

func VerifyPassword(encoded, password string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" || parts[2] != "v=19" {
		return false
	}
	var memory uint32
	var iterations uint32
	var parallelism uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &iterations, &parallelism); err != nil {
		return false
	}
	if memory < 8 || memory > 1024*1024 || iterations < 1 || iterations > 32 || parallelism < 1 || parallelism > 32 {
		return false
	}
	salt, err := base64.RawStdEncoding.Strict().DecodeString(parts[4])
	if err != nil || len(salt) < 8 || len(salt) > 64 {
		return false
	}
	expected, err := base64.RawStdEncoding.Strict().DecodeString(parts[5])
	if err != nil || len(expected) < 16 || len(expected) > 64 {
		return false
	}
	actual := argon2.IDKey([]byte(password), salt, iterations, memory, parallelism, uint32(len(expected)))
	return subtle.ConstantTimeCompare(actual, expected) == 1
}

func EnsureBootstrapAdmin(ctx context.Context, db *sql.DB, username, password string) error {
	var count int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(1) FROM users").Scan(&count); err != nil {
		return fmt.Errorf("count users: %w", err)
	}
	if count != 0 {
		return nil
	}
	if password == "" {
		return errors.New("BOOTSTRAP_PASSWORD is required when the database has no users")
	}
	hash, err := HashPassword(password)
	if err != nil {
		return err
	}
	now := time.Now().UnixMilli()
	_, err = db.ExecContext(ctx, `INSERT INTO users(
		username, password_hash, role, token_version, expires_at, flow_quota_bytes,
		forward_quota, status, created_at, updated_at
	) VALUES(?, ?, 'admin', 1, ?, 0, 0, 1, ?, ?)`, username, hash, time.Now().AddDate(20, 0, 0).UnixMilli(), now, now)
	if err != nil {
		return fmt.Errorf("create bootstrap admin: %w", err)
	}
	return nil
}
