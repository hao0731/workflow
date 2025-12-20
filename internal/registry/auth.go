package registry

import (
	"crypto/sha256"
	"encoding/hex"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// JWTConfig holds JWT configuration.
type JWTConfig struct {
	Secret     []byte
	Issuer     string
	Expiration time.Duration
}

// DefaultJWTConfig returns default JWT settings.
func DefaultJWTConfig(secret string) JWTConfig {
	return JWTConfig{
		Secret:     []byte(secret),
		Issuer:     "orchestration-registry",
		Expiration: 24 * time.Hour,
	}
}

// NodeClaims are the JWT claims for node workers.
type NodeClaims struct {
	jwt.RegisteredClaims
	NodeType     string `json:"node_type"`
	FullType     string `json:"full_type"`
	Subject      string `json:"subject"`
	ConsumerName string `json:"consumer_name"`
}

// GenerateToken creates a JWT for a registered node.
func GenerateToken(cfg JWTConfig, reg *NodeRegistration) (string, error) {
	now := time.Now()
	claims := NodeClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    cfg.Issuer,
			Subject:   reg.ID,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(cfg.Expiration)),
		},
		NodeType:     reg.NodeType,
		FullType:     reg.FullType,
		Subject:      reg.Subject,
		ConsumerName: reg.ConsumerName,
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(cfg.Secret)
}

// ValidateToken parses and validates a JWT, returning the claims.
func ValidateToken(cfg JWTConfig, tokenString string) (*NodeClaims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &NodeClaims{}, func(token *jwt.Token) (any, error) {
		return cfg.Secret, nil
	})
	if err != nil {
		return nil, err
	}

	claims, ok := token.Claims.(*NodeClaims)
	if !ok || !token.Valid {
		return nil, jwt.ErrTokenInvalidClaims
	}

	return claims, nil
}

// HashToken creates a SHA-256 hash of a token for storage.
func HashToken(token string) string {
	hash := sha256.Sum256([]byte(token))
	return hex.EncodeToString(hash[:])
}
