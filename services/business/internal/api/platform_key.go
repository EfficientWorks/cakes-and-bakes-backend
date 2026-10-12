package api

import (
	"crypto/subtle"
	"net/http"

	"bakehouse/pkg/apperr"
	"bakehouse/pkg/httpx"
)

// PlatformKeyHeader carries the emzo team's platform key.
const PlatformKeyHeader = "X-Platform-Key"

/*
 * RequirePlatformKey guards the platform API: registering and managing
 * businesses is for the emzo team, not for any business's staff.
 *
 * The key comes from PLATFORM_API_KEY. Without one configured the whole API
 * answers 503, so a deployment that forgot to set it is closed rather than
 * open. The comparison is constant-time so the key cannot be guessed a byte
 * at a time from response timings.
 */
func RequirePlatformKey(key string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if key == "" {
				httpx.WriteError(w, r, apperr.New("PLATFORM_API_DISABLED", apperr.CategoryUpstream,
					"The platform API is not enabled on this server. Set PLATFORM_API_KEY."))
				return
			}
			given := r.Header.Get(PlatformKeyHeader)
			if subtle.ConstantTimeCompare([]byte(given), []byte(key)) != 1 {
				httpx.WriteError(w, r, apperr.Unauthenticated("A valid "+PlatformKeyHeader+" header is required."))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
