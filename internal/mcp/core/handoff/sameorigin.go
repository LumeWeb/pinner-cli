package handoff

import (
	"net/http"
	"net/url"
	"strings"
)

// SameOrigin reports whether an inbound request originates from one of the
// given acceptable origins. Browser-only endpoints (login, OOB pages) use this
// to reject cross-origin web pages and non-browser clients: a browser form POST
// always carries an Origin header (and usually a Referer), so a request whose
// Origin matches none of the accepted origins, or that carries neither header,
// is rejected. This blocks CSRF from a cross-origin web page.
func SameOrigin(r *http.Request, accepted ...string) bool {
	matches := func(candidate string) bool {
		if candidate == "" {
			return false
		}
		for _, a := range accepted {
			if strings.EqualFold(candidate, a) {
				return true
			}
		}
		return false
	}
	if origin := r.Header.Get("Origin"); origin != "" {
		return matches(origin)
	}
	if referer := r.Header.Get("Referer"); referer != "" {
		u, err := url.Parse(referer)
		if err != nil {
			return false
		}
		return matches(u.Scheme + "://" + u.Host)
	}
	return false
}
