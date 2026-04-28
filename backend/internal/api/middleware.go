package api

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/golang-jwt/jwt/v5"
)

// ctxKey is the context key for auth claims.
type ctxKey struct{}

// AuthClaims holds the fields extracted from a validated JWT.
type AuthClaims struct {
	Email string
	OrgID int64
	Role  string
}

// jwtClaims maps the Python-issued JWT payload.
// Python creates: {"sub": email, "org_id": org_id, "role": role, "exp": ...}
type jwtClaims struct {
	jwt.RegisteredClaims
	OrgID int64  `json:"org_id"`
	Role  string `json:"role"`
}

// requireAuth is middleware that validates the Bearer JWT and injects AuthClaims into context.
func (s *Server) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tokenStr, err := bearerToken(r)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "missing or malformed Authorization header")
			return
		}

		claims := &jwtClaims{}
		_, err = jwt.ParseWithClaims(tokenStr, claims, func(t *jwt.Token) (any, error) {
			if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
			}
			return []byte(s.cfg.JWTSecret), nil
		})
		if err != nil {
			writeError(w, http.StatusUnauthorized, "invalid or expired token")
			return
		}

		ac := AuthClaims{
			Email: claims.Subject, // Python sets sub = email
			OrgID: claims.OrgID,
			Role:  claims.Role,
		}
		ctx := context.WithValue(r.Context(), ctxKey{}, ac)
		next.ServeHTTP(w, r.WithContext(ctx))
	}
}

// getAuth retrieves AuthClaims from the request context.
func getAuth(r *http.Request) AuthClaims {
	v, _ := r.Context().Value(ctxKey{}).(AuthClaims)
	return v
}

// bearerToken extracts the token string from "Authorization: Bearer <token>"
// or from the "token" query parameter (used by EventSource/SSE which can't set headers).
func bearerToken(r *http.Request) (string, error) {
	header := r.Header.Get("Authorization")
	if header != "" {
		parts := strings.SplitN(header, " ", 2)
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
			return "", fmt.Errorf("expected Bearer token")
		}
		return parts[1], nil
	}
	if t := r.URL.Query().Get("token"); t != "" {
		return t, nil
	}
	return "", fmt.Errorf("no Authorization header")
}
