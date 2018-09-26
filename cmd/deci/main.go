package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"log"
	"os"
	"runtime"

	"net/http"

	"crypto/rand"

	"github.com/gorilla/sessions"
	"github.com/heroku/deci"
	"github.com/heroku/deci/internal/server"
	"github.com/heroku/deci/internal/storage"
	"github.com/heroku/deci/internal/storage/sql"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

const (
	sessionAuthenticationKeyBytesLength = 64
	sessionEncryptionKeyBytesLength     = 32
)

func main() {
	if err := cmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "%s: %s\n", os.Args[0], err)
		os.Exit(1)
	}
}

var cmd = cobra.Command{
	RunE: run,
}

var ( // flags
	addr                     string
	scfg                     server.Config
	sessionAuthenticationKey string
	sessionEncryptionKey     string
	dbURL                    string
)

func init() {
	cmd.Flags().StringVar(&addr, "addr", "localhost:5556", "Address to listen on")
	cmd.Flags().StringVar(&scfg.Issuer, "issuer", "http://localhost:5556", "Issuer URL for OIDC provider")
	cmd.Flags().StringVar(&sessionAuthenticationKey, "session-auth-key", mustGenRandB64(64), "Session authentication key, 64-byte, base64-encoded")
	cmd.Flags().StringVar(&sessionEncryptionKey, "session-encrypt-key", mustGenRandB64(32), "Session encryption key, 32-byte, base64-encoded")
	cmd.Flags().StringVar(&dbURL, "database", defaultDBUrl(), "URL to postgres database for persistence")
}

func run(cmd *cobra.Command, args []string) error {
	logger := logrus.New()

	sessionAuthenticationKey, err := base64.StdEncoding.DecodeString(sessionAuthenticationKey)
	if err != nil {
		return errors.Wrap(err, "failed to base64 decode session-auth-key")
	} else if len(sessionAuthenticationKey) != sessionAuthenticationKeyBytesLength {
		return fmt.Errorf("session-auth-key must be %d bytes of random data", sessionAuthenticationKeyBytesLength)
	}

	sessionEncryptionKey, err := base64.StdEncoding.DecodeString(sessionEncryptionKey)
	if err != nil {
		return errors.Wrap(err, "failed to base64 decode session-encrypt-key")
	} else if len(sessionEncryptionKey) != sessionEncryptionKeyBytesLength {
		return fmt.Errorf("session-encrypt-key must be %d bytes of random data", sessionEncryptionKeyBytesLength)
	}

	// Configure OIDC sderver

	scfg.Logger = logger
	scfg.PrometheusRegistry = prometheus.NewRegistry() // TODO: Actually register stuff to this
	scfg.AuthPrefix = "/auth"                          // where we want incoming requests from clients to land

	store, err := sql.PostgresForURL(logger, dbURL)
	if err != nil {
		return errors.Wrap(err, "failed to configure storage")
	}

	scfg.Storage = storage.WithStaticClients(store, []storage.Client{
		{
			Name:   "Example App",
			ID:     "example-app",
			Secret: "ZXhhbXBsZS1hcHAtc2VjcmV0",
			RedirectURIs: []string{
				"http://127.0.0.1:5555/callback",
			},
		},
	})

	server, err := server.NewServer(context.Background(), &scfg)
	if err != nil {
		return err
	}

	session := sessions.NewCookieStore(sessionAuthenticationKey, sessionEncryptionKey)

	// TODO - load config from somewhere
	a, err := deci.NewApp(logger, server, session)
	if err != nil {
		return errors.Wrap(err, "Error creating app")
	}

	srv := &http.Server{
		Addr:    addr,
		Handler: a,
	}
	logger.WithField("addr", addr).Info("starting")
	return srv.ListenAndServe()
}

func mustGenRandB64(len int) string {
	b := make([]byte, len)
	_, err := rand.Read(b)
	if err != nil {
		log.Fatalf("Error fetching %d random bytes [%+v]", len, err)
	}
	return base64.StdEncoding.EncodeToString(b)
}

func defaultDBUrl() string {
	// socket stuff seems weird on mac, but network is on by default in brew
	if runtime.GOOS == "darwin" {
		return "postgres://127.0.0.1/deci?sslmode=disable"
	}
	return "postgres:///deci"
}
