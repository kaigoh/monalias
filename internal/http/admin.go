package httpx

import (
	"crypto/subtle"
	"net/http"

	"github.com/kaigoh/monalias/internal/config"
	"github.com/kaigoh/monalias/internal/ui"
)

func AdminHandler(cfg config.Config, gqlHandler http.Handler) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/graphql", basicAuth(cfg, gqlHandler))
	mux.Handle("/", basicAuth(cfg, ui.Handler()))
	return mux
}

func basicAuth(cfg config.Config, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		if !ok || !checkCreds(user, pass, cfg.AdminUser, cfg.AdminPassword) {
			w.Header().Set("WWW-Authenticate", `Basic realm="Monalias"`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func checkCreds(user, pass, expectedUser, expectedPass string) bool {
	if expectedUser == "" || expectedPass == "" {
		return false
	}
	userMatch := subtle.ConstantTimeCompare([]byte(user), []byte(expectedUser)) == 1
	passMatch := subtle.ConstantTimeCompare([]byte(pass), []byte(expectedPass)) == 1
	return userMatch && passMatch
}
