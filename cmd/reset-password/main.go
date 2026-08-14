package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/cyxc1124/osspilot-ops-api/internal/auth"
	"github.com/cyxc1124/osspilot-ops-api/internal/config"
	"github.com/cyxc1124/osspilot-ops-api/internal/users"
)

const defaultUsername = "admin"

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	fs := flag.NewFlagSet("reset-password", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	username := fs.String("username", "", "ops username (default admin, or OPS_RESET_USERNAME)")
	fs.StringVar(username, "u", "", "ops username")
	password := fs.String("password", "", "new password (prefer OPS_RESET_PASSWORD)")
	fs.StringVar(password, "p", "", "new password")
	list := fs.Bool("list", false, "list ops users and exit")
	fs.BoolVar(list, "l", false, "list ops users")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	cfg := config.Load()
	if cfg.DatabaseURL == "" {
		fmt.Fprintln(os.Stderr, "DATABASE_URL is required")
		return 1
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "db: %v\n", err)
		return 1
	}
	defer pool.Close()
	store := users.NewStore(pool)

	if *list {
		return listUsers(ctx, store)
	}

	name := strings.TrimSpace(*username)
	if name == "" {
		name = strings.TrimSpace(os.Getenv("OPS_RESET_USERNAME"))
	}
	if name == "" {
		name = defaultUsername
	}

	pass, err := resolvePassword(*password)
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		fmt.Fprintln(os.Stderr, "Provide --password, set OPS_RESET_PASSWORD, or pipe the password on stdin.")
		return 1
	}

	user, err := store.GetByUsername(ctx, name)
	if err != nil {
		fmt.Fprintf(os.Stderr, "db: %v\n", err)
		return 1
	}
	if user == nil {
		fmt.Fprintf(os.Stderr, "Ops user not found: %q\n", name)
		fmt.Fprintln(os.Stderr, "Use --list to see available users.")
		return 1
	}
	hash, err := auth.HashPassword(pass)
	if err != nil {
		fmt.Fprintf(os.Stderr, "hash: %v\n", err)
		return 1
	}
	if err := store.UpdatePassword(ctx, user.ID, hash, time.Now().UTC()); err != nil {
		fmt.Fprintf(os.Stderr, "db: %v\n", err)
		return 1
	}
	fmt.Fprintf(os.Stderr, "Password reset for ops user id=%d username=%q status=%s\n", user.ID, user.Username, user.Status)
	return 0
}

func listUsers(ctx context.Context, store *users.Store) int {
	items, err := store.List(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "db: %v\n", err)
		return 1
	}
	if len(items) == 0 {
		fmt.Fprintln(os.Stderr, "No ops users found.")
		return 0
	}
	for _, u := range items {
		display := ""
		if u.DisplayName != nil {
			display = *u.DisplayName
		}
		fmt.Printf("%d\t%s\t%s\t%s\n", u.ID, u.Username, u.Status, display)
	}
	return 0
}

// ponytail: 不接 TTY 隐式输入（免 x/term）。交互用 --password、OPS_RESET_PASSWORD，或非 TTY stdin。
func resolvePassword(cli string) (string, error) {
	p := strings.TrimSpace(cli)
	if p == "" {
		p = strings.TrimSpace(os.Getenv("OPS_RESET_PASSWORD"))
	}
	if p == "" && !stdinIsTTY() {
		b, err := io.ReadAll(io.LimitReader(os.Stdin, 256))
		if err != nil {
			return "", err
		}
		p = strings.TrimSpace(string(b))
	}
	return validatePassword(p)
}

func validatePassword(password string) (string, error) {
	if password == "" {
		return "", fmt.Errorf("Password is required")
	}
	if len(password) < 8 || len(password) > 128 {
		return "", fmt.Errorf("Password must be 8-128 characters")
	}
	return password, nil
}

func stdinIsTTY() bool {
	fi, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}
