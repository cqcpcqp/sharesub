package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/sharesub/sharesub/backend/internal/application"
	"github.com/sharesub/sharesub/backend/internal/config"
	"github.com/sharesub/sharesub/backend/internal/postgres"
)

func main() {
	if len(os.Args) < 2 || os.Args[1] != "reset-password" {
		fmt.Fprintln(os.Stderr, "usage: sharesub-admin reset-password [admin-email]")
		os.Exit(2)
	}
	cfg, err := config.Load()
	if err != nil {
		fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	store, err := postgres.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		fatal(err)
	}
	defer store.Close()
	if err := store.Migrate(ctx); err != nil {
		fatal(err)
	}
	service := application.NewService(store, nil, nil, 0, "", "")
	email := application.BootstrapAdminEmail
	if len(os.Args) >= 3 {
		email = os.Args[2]
	}
	credential, err := service.ResetAdminPassword(ctx, email)
	if err != nil {
		fatal(err)
	}
	fmt.Printf("admin password reset\nemail=%s\ntemporary_password=%s\n", credential.Email, credential.TemporaryPassword)
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
