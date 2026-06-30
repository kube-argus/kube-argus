// Command server runs the Google Workspace OIDC broker.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrlconfig "sigs.k8s.io/controller-runtime/pkg/client/config"

	gwoidciov1 "github.com/kube-argus/kube-argus/operator/api/v1"
	"github.com/kube-argus/kube-argus/service/internal/broker"
	"github.com/kube-argus/kube-argus/service/internal/config"
	"github.com/kube-argus/kube-argus/service/internal/idp"
	"github.com/kube-argus/kube-argus/service/internal/k8s"
	"github.com/kube-argus/kube-argus/service/internal/server"
	"github.com/kube-argus/kube-argus/service/internal/store"
)

const waitBindedTimeout = 15 * time.Second

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

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	// Kubernetes clients (in-cluster or KUBECONFIG).
	restCfg, err := ctrlconfig.GetConfig()
	if err != nil {
		return err
	}
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		return err
	}
	if err := gwoidciov1.AddToScheme(scheme); err != nil {
		return err
	}
	crClient, err := client.New(restCfg, client.Options{Scheme: scheme})
	if err != nil {
		return err
	}
	clientset, err := kubernetes.NewForConfig(restCfg)
	if err != nil {
		return err
	}

	st, err := store.New(cfg.Store)
	if err != nil {
		return err
	}
	defer func() { _ = st.Close() }()

	provider, err := idp.New(ctx, cfg)
	if err != nil {
		return err
	}

	binder := k8s.NewBinder(crClient, cfg.BindNamespace, cfg.BindTTL, waitBindedTimeout)
	minter := k8s.NewMinter(clientset, cfg.BindNamespace, cfg.TokenAudiences, int64(cfg.TokenLifetime.Seconds()))

	b, err := broker.New(cfg, st, provider, binder, minter, log)
	if err != nil {
		return err
	}
	srv := server.New(cfg.ListenAddr, b, log)

	tlsEnabled := cfg.TLSCertFile != "" && cfg.TLSKeyFile != ""
	errCh := make(chan error, 1)
	go func() {
		log.Info("broker listening", "addr", cfg.ListenAddr, "issuer", cfg.Issuer, "tls", tlsEnabled)
		var err error
		if tlsEnabled {
			err = srv.ListenAndServeTLS(cfg.TLSCertFile, cfg.TLSKeyFile)
		} else {
			err = srv.ListenAndServe()
		}
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		log.Info("shutting down")
		shutCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return srv.Shutdown(shutCtx)
	}
}
