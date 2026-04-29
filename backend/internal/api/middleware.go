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
//
// Kind is empty for the regular long-lived auth JWT and "sse" for the
// short-lived ticket minted by /api/sse/ticket — the SSE-specific auth path
// rejects anything except kind="sse" so a leaked auth JWT can't be
// downgraded into a query-string ticket. (issue #80)
type jwtClaims struct {
	jwt.RegisteredClaims
	OrgID int64  `json:"org_id"`
	Role  string `json:"role"`
	Kind  string `json:"kind,omitempty"`
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

// requireRole wraps requireAuth and additionally enforces that the
// authenticated user's role is one of the allowed values. Returns 403 with
// no body details on mismatch — we don't tell the caller which role they
// would need.
//
// Existing JWTs minted before role was added to the claims may have an empty
// Role field; in that case we fall back to a single DB lookup so a long-lived
// token doesn't accidentally bypass authorization. Subsequent re-logins
// embed the role and skip the lookup.
func (s *Server) requireRole(allowed ...string) func(http.HandlerFunc) http.HandlerFunc {
	return func(next http.HandlerFunc) http.HandlerFunc {
		return s.requireAuth(func(w http.ResponseWriter, r *http.Request) {
			ac := getAuth(r)
			role := ac.Role
			if role == "" && s.db != nil && ac.Email != "" {
				if u, err := s.db.GetUserByEmail(ac.Email); err == nil && u != nil {
					role = u.Role
				}
			}
			for _, want := range allowed {
				if role == want {
					next.ServeHTTP(w, r)
					return
				}
			}
			writeError(w, http.StatusForbidden, "forbidden")
		})
	}
}

// bearerToken extracts the token string from "Authorization: Bearer <token>".
// Query-string fallback was removed — the long-lived auth JWT must never
// appear in URLs because reverse proxies, browser history, and Referer
// headers leak query strings. (issue #80) For SSE / <audio> tag callers
// that cannot set headers, see requireSSETicket and the blob-fetch pattern
// on the frontend.
func bearerToken(r *http.Request) (string, error) {
	header := r.Header.Get("Authorization")
	if header == "" {
		return "", fmt.Errorf("no Authorization header")
	}
	parts := strings.SplitN(header, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return "", fmt.Errorf("expected Bearer token")
	}
	return parts[1], nil
}

// requireSSETicket is a middleware variant for SSE endpoints. It reads a
// short-lived ticket from the ?ticket= query (because EventSource cannot
// send custom headers) and accepts ONLY tokens with kind="sse" — the
// long-lived auth JWT is rejected here even if smuggled in. The ticket is
// minted via GET /api/sse/ticket which itself requires Bearer auth.
func (s *Server) requireSSETicket(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		t := r.URL.Query().Get("ticket")
		if t == "" {
			writeError(w, http.StatusUnauthorized, "missing ticket")
			return
		}
		claims := &jwtClaims{}
		_, err := jwt.ParseWithClaims(t, claims, func(tok *jwt.Token) (any, error) {
			if _, ok := tok.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, fmt.Errorf("unexpected signing method: %v", tok.Header["alg"])
			}
			return []byte(s.cfg.JWTSecret), nil
		})
		if err != nil || claims.Kind != "sse" {
			writeError(w, http.StatusUnauthorized, "invalid or expired ticket")
			return
		}
		ac := AuthClaims{Email: claims.Subject, OrgID: claims.OrgID, Role: claims.Role}
		ctx := context.WithValue(r.Context(), ctxKey{}, ac)
		next.ServeHTTP(w, r.WithContext(ctx))
	}
}
