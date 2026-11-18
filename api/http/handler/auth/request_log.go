package auth

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/felixge/httpsnoop"
	portainer "github.com/portainer/portainer/api"
	"github.com/portainer/portainer/api/authlog"
	"github.com/portainer/portainer/api/http/security"
)

type authLogKey struct{}

type authLogRequest struct {
	username      string
	method        int
	userID        int
	authenticated bool
	rateLimited   bool
}

const rateLimitLogInterval = 10 * time.Second

// sampleRateLimited bounds persistent writes globally, including floods from
// many origins. Suppressed requests are counted in the next sampled record.
func (handler *Handler) sampleRateLimited(now time.Time) (bool, uint64) {
	handler.rateLimitLogMu.Lock()
	defer handler.rateLimitLogMu.Unlock()
	if now.Before(handler.rateLimitLogNext) {
		// Saturate rather than wrap if the counter reaches its limit.
		if handler.rateLimitLogSuppressed < ^uint64(0) {
			handler.rateLimitLogSuppressed++
		}
		return false, 0
	}
	suppressed := handler.rateLimitLogSuppressed
	handler.rateLimitLogSuppressed = 0
	handler.rateLimitLogNext = now.Add(rateLimitLogInterval)
	return true, suppressed
}

func markRateLimited(r *http.Request) {
	if info := logRequest(r); info != nil {
		info.rateLimited = true
	}
}

func logRequest(r *http.Request) *authLogRequest {
	info, _ := r.Context().Value(authLogKey{}).(*authLogRequest)
	return info
}

func setLogUser(r *http.Request, user *portainer.User) {
	if info := logRequest(r); info != nil && user != nil && user.ID != 0 {
		info.username = safeUsername(user.Username)
		info.userID = int(user.ID)
	}
}

// Limit attacker-controlled usernames without ever reading or storing bodies.
func safeUsername(name string) string {
	runes := []rune(strings.ToValidUTF8(name, ""))
	if len(runes) > 512 {
		runes = runes[:512]
	}
	return string(runes)
}

func (handler *Handler) recordAuth(limiter *security.RateLimiter, next http.Handler, method int, logout bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		info := &authLogRequest{method: method}
		r = r.WithContext(context.WithValue(r.Context(), authLogKey{}, info))
		status := http.StatusInternalServerError
		defer func() {
			if handler.AuthLog == nil || (logout && !info.authenticated) {
				return
			}
			var suppressed uint64
			if info.rateLimited {
				var sample bool
				sample, suppressed = handler.sampleRateLimited(time.Now())
				if !sample {
					return
				}
			}
			eventType := authlog.Failure
			if logout {
				eventType = authlog.Logout
			} else if status >= 200 && status < 300 {
				eventType = authlog.Success
			}
			_ = handler.AuthLog.Append(authlog.Event{
				Username: info.username, UserID: info.userID, Context: info.method,
				Type: eventType, Origin: limiter.ClientIP(r), StatusCode: status,
				RateLimited: info.rateLimited, SuppressedRateLimited: suppressed,
			})
		}()
		metrics := httpsnoop.CaptureMetrics(next, w, r)
		status = metrics.Code
	})
}
