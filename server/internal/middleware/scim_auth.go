package middleware

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// SCIMTokenPrefix marks a provisioning token (K60). SCIM tokens live outside
// middleware.Auth on purpose: they can only reach /scim/v2 and never an
// /api/* handler, and no browser session can reach /scim/v2 with a cookie.
const SCIMTokenPrefix = "scim_"

// HashSCIMToken is the stored form of a SCIM token.
func HashSCIMToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// SCIMBearerOnly authenticates a provisioning request and stamps the
// workspace it may provision.
func SCIMBearerOnly(queries *db.Queries) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.Header.Del("X-Workspace-ID")
			r.Header.Del("X-Scim-Token-ID")
			header := r.Header.Get("Authorization")
			token := strings.TrimSpace(strings.TrimPrefix(header, "Bearer "))
			if !strings.HasPrefix(header, "Bearer ") || !strings.HasPrefix(token, SCIMTokenPrefix) {
				writeSCIMError(w, http.StatusUnauthorized, "a SCIM bearer token is required")
				return
			}
			row, err := queries.GetActiveScimTokenByHash(r.Context(), HashSCIMToken(token))
			if err != nil {
				writeSCIMError(w, http.StatusUnauthorized, "invalid SCIM token")
				return
			}
			_ = queries.TouchScimToken(r.Context(), row.ID)
			r.Header.Set("X-Workspace-ID", uuidToString(row.WorkspaceID))
			r.Header.Set("X-Scim-Token-ID", uuidToString(row.ID))
			next.ServeHTTP(w, r)
		})
	}
}

func writeSCIMError(w http.ResponseWriter, status int, detail string) {
	w.Header().Set("Content-Type", "application/scim+json")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(`{"schemas":["urn:ietf:params:scim:api:messages:2.0:Error"],"status":"` + http.StatusText(status) + `","detail":"` + strings.ReplaceAll(detail, `"`, `'`) + `"}`))
}
