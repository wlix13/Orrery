package api

import (
	"context"
	"crypto/subtle"
	"net/http"
	"strings"

	"github.com/wlix13/orrery/collector/internal/config"
	"github.com/wlix13/orrery/collector/internal/store"
)

// Authentication methods, as reported by /api/me.
const (
	methodToken     = "token"
	methodAnonymous = "anonymous"
)

// Principal is the authenticated caller and the fleets it may read.
type Principal struct {
	Name   string
	Method string
	Scope  store.Scope
}

type principalKey struct{}

// principalOf returns the caller resolved by the auth middleware. The zero
// Scope matches nothing, so a handler reached without the middleware reads no
// data rather than everything.
func principalOf(ctx context.Context) Principal {
	p, _ := ctx.Value(principalKey{}).(Principal)
	return p
}

func scopeOf(fleets []string) store.Scope {
	if len(fleets) == 0 {
		return store.AllFleets()
	}

	return store.FleetScope(fleets...)
}

// auth resolves the request's credential and rejects it if there is none.
//
// Fails closed: only auth.allow_anonymous skips the check, so a missing or
// unknown credential rejects the request rather than serving the fleet's
// traffic history.
func (s *Server) auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, ok := s.authenticate(r)
		if !ok {
			writeErr(w, http.StatusUnauthorized, "unauthorized", "missing or invalid credentials")
			return
		}

		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), principalKey{}, p)))
	})
}

func (s *Server) authenticate(r *http.Request) (Principal, bool) {
	if s.cfg.Auth.AllowAnonymous {
		return Principal{Name: methodAnonymous, Method: methodAnonymous, Scope: store.AllFleets()}, true
	}

	token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok || token == "" {
		return Principal{}, false
	}

	return s.tokenPrincipal(token)
}

// tokenPrincipal compares against every configured token so the work does not
// depend on which one matched.
func (s *Server) tokenPrincipal(token string) (Principal, bool) {
	var found *config.TokenConfig

	for i := range s.cfg.Auth.Tokens {
		t := &s.cfg.Auth.Tokens[i]
		if subtle.ConstantTimeCompare([]byte(token), []byte(t.Token)) == 1 {
			found = t
		}
	}

	if found == nil {
		return Principal{}, false
	}

	return Principal{Name: found.Name, Method: methodToken, Scope: scopeOf(found.Fleets)}, true
}

// handleMe lets the dashboard learn who it is without a token gate, and which
// fleets it will be shown.
func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	p := principalOf(r.Context())

	fleets := p.Scope.Fleets()
	if p.Scope.All() {
		fleets = nil
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"name":   p.Name,
		"method": p.Method,
		"fleets": fleets, // null means every fleet
	})
}
