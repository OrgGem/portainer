package auth

import (
	"net/http"
	"sync"
	"time"

	portainer "github.com/portainer/portainer/api"
	"github.com/portainer/portainer/api/authlog"
	"github.com/portainer/portainer/api/dataservices"
	"github.com/portainer/portainer/api/http/proxy"
	"github.com/portainer/portainer/api/http/proxy/factory/kubernetes"
	"github.com/portainer/portainer/api/http/security"
	"github.com/portainer/portainer/api/kubernetes/cli"
	httperror "github.com/portainer/portainer/pkg/libhttp/error"

	"github.com/gorilla/mux"
)

// Handler is the HTTP handler used to handle authentication operations.
type Handler struct {
	*mux.Router
	DataStore                   dataservices.DataStore
	CryptoService               portainer.CryptoService
	JWTService                  portainer.JWTService
	LDAPService                 portainer.LDAPService
	OAuthService                portainer.OAuthService
	AuthLog                     *authlog.Store
	ProxyManager                *proxy.Manager
	KubernetesTokenCacheManager *kubernetes.TokenCacheManager
	KubernetesClientFactory     *cli.ClientFactory
	passwordStrengthChecker     security.PasswordStrengthChecker
	bouncer                     security.BouncerService
	rateLimitLogMu              sync.Mutex
	rateLimitLogNext            time.Time
	rateLimitLogSuppressed      uint64
}

// NewHandler creates a handler to manage authentication operations.
func NewHandler(bouncer security.BouncerService, rateLimiter *security.RateLimiter, passwordStrengthChecker security.PasswordStrengthChecker, kubernetesClientFactory *cli.ClientFactory) *Handler {
	h := &Handler{
		Router:                  mux.NewRouter(),
		passwordStrengthChecker: passwordStrengthChecker,
		bouncer:                 bouncer,
		KubernetesClientFactory: kubernetesClientFactory,
	}

	h.Handle("/auth/oauth/validate", h.recordAuth(rateLimiter,
		rateLimiter.LimitAccessWithRejection(bouncer.PublicAccess(httperror.LoggerHandler(h.validateOAuth)), markRateLimited), authlog.OAuth, false)).Methods(http.MethodPost)
	h.Handle("/auth", h.recordAuth(rateLimiter,
		rateLimiter.LimitAccessWithRejection(bouncer.PublicAccess(httperror.LoggerHandler(h.authenticate)), markRateLimited), authlog.Unknown, false)).Methods(http.MethodPost)
	h.Handle("/auth/logout", h.recordAuth(rateLimiter,
		bouncer.PublicAccess(httperror.LoggerHandler(h.logout)), authlog.Unknown, true)).Methods(http.MethodPost)

	return h
}
