package auth

import (
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"anpr-service/internal/model"
)

type Claims struct {
	SessionID uuid.UUID      `json:"sid"`
	UserID    uuid.UUID      `json:"sub"`
	Role      model.UserRole `json:"role"`
	OrgID     uuid.UUID      `json:"org_id"`
	DriverID  *uuid.UUID     `json:"driver_id,omitempty"`
	jwt.RegisteredClaims
}

type Parser struct {
	secret []byte
}

func NewParser(secret string) *Parser {
	return &Parser{secret: []byte(secret)}
}

func (p *Parser) Parse(tokenStr string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenStr, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		return p.secret, nil
	})
	if err != nil {
		return nil, err
	}

	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, jwt.ErrTokenInvalidClaims
	}

	return claims, nil
}

