// Command proxy runs the kargus kube-apiserver auth proxy: it verifies broker
// id_tokens and forwards requests to the apiserver as the user's SA token.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	ctrlconfig "sigs.k8s.io/controller-runtime/pkg/client/config"

	"github.com/kube-argus/kube-argus/service/internal/k8s"
	"github.com/kube-argus/kube-argus/service/internal/proxy"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(log); err != nil {
		log.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	var (
		issuer        = os.Getenv("ISSUER")
		clientID      = os.Getenv("CLIENT_ID")
		namespace     = env("BIND_NAMESPACE", "kargus-system")
		listen        = env("LISTEN_ADDR", ":8443")
		usernameClaim = env("USERNAME_CLAIM", "email")
		expSecs       = envInt("TOKEN_EXPIRATION_SECONDS", 600)
		certFile      = os.Getenv("TLS_CERT_FILE")
		keyFile       = os.Getenv("TLS_KEY_FILE")
	)
	if issuer == "" || clientID == "" {
		return errors.New("ISSUER and CLIENT_ID are required")
	}

	// Verify broker id_tokens against the broker's JWKS. Use a lazy remote key set
	// + explicit verifier (not oidc.NewProvider) so the proxy starts even when the
	// broker isn't reachable yet, and so JWKS can be fetched from the broker's
	// in-cluster Service (no external DNS / ingress hairpin). The `iss` claim is
	// still checked against the public ISSUER.
	jwksURL := env("JWKS_URL", strings.TrimRight(issuer, "/")+"/jwks")
	keySet := oidc.NewRemoteKeySet(ctx, jwksURL)
	verifier := oidc.NewVerifier(issuer, keySet, &oidc.Config{ClientID: clientID})
	log.Info("verifying id_tokens", "issuer", issuer, "jwks", jwksURL)

	// Kubernetes: client to mint SA tokens, and a TLS-only transport to the apiserver.
	restCfg, err := ctrlconfig.GetConfig()
	if err != nil {
		return err
	}
	clientset, err := kubernetes.NewForConfig(restCfg)
	if err != nil {
		return err
	}
	minter := k8s.NewMinter(clientset, namespace, nil, int64(expSecs))

	apiURL, err := url.Parse(restCfg.Host)
	if err != nil {
		return err
	}
	tlsConf, err := rest.TLSConfigFor(restCfg)
	if err != nil {
		return err
	}
	if tlsConf != nil {
		tlsConf.NextProtos = []string{"http/1.1"} // SPDY (exec/attach/port-forward) needs HTTP/1.1
	}
	transport := &http.Transport{TLSClientConfig: tlsConf, ForceAttemptHTTP2: false}

	cacheTTL := time.Duration(float64(expSecs)*0.8) * time.Second
	p := proxy.New(verifier, minter, apiURL, transport, usernameClaim, cacheTTL, log)

	srv := &http.Server{
		Addr:              listen,
		Handler:           p,
		ReadHeaderTimeout: 10 * time.Second,
		// No WriteTimeout: kube streams (logs -f, exec, port-forward) are long-lived.
		IdleTimeout: 120 * time.Second,
	}

	tlsEnabled := certFile != "" && keyFile != ""
	errCh := make(chan error, 1)
	go func() {
		log.Info("kargus-proxy listening", "addr", listen, "apiserver", apiURL.String(), "tls", tlsEnabled)
		var serr error
		if tlsEnabled {
			serr = srv.ListenAndServeTLS(certFile, keyFile)
		} else {
			serr = srv.ListenAndServe()
		}
		if serr != nil && !errors.Is(serr, http.ErrServerClosed) {
			errCh <- serr
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		shutCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return srv.Shutdown(shutCtx)
	}
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}
