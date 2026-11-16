package cmd

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"github.com/swadhinbiswas/ghost/internal/remote"
)

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Control Ghost through a local HTTP API",
	Long:  "Run Ghost without the terminal UI and accept authenticated session control requests.",
	RunE: func(cmd *cobra.Command, _ []string) error {
		bind, _ := cmd.Flags().GetString("bind")
		port, _ := cmd.Flags().GetInt("port")
		token, _ := cmd.Flags().GetString("token")
		if token == "" {
			token = os.Getenv("GHOST_REMOTE_TOKEN")
		}
		loadedToken, err := remote.LoadToken(token)
		if err != nil {
			return err
		}

		listener, err := remote.Listen(bind, port)
		if err != nil {
			return fmt.Errorf("could not listen on %s:%d: %w", bind, port, err)
		}
		defer listener.Close()

		ghostApp, err := setupApp(cmd)
		if err != nil {
			return err
		}
		defer ghostApp.Shutdown()

		ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		server := &http.Server{
			Handler:           remote.NewServer(ctx, ghostApp, loadedToken).Handler(),
			ReadHeaderTimeout: 5 * time.Second,
		}
		if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Ghost remote API listening at http://%s\nBearer token: %s\n", listener.Addr(), loadedToken); err != nil {
			return err
		}

		serveErr := make(chan error, 1)
		go func() { serveErr <- server.Serve(listener) }()
		select {
		case err := <-serveErr:
			if !errors.Is(err, http.ErrServerClosed) {
				return fmt.Errorf("remote server stopped: %w", err)
			}
		case <-ctx.Done():
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := server.Shutdown(shutdownCtx); err != nil {
				return fmt.Errorf("shut down remote server: %w", err)
			}
			<-serveErr
		}
		return nil
	},
}

func init() {
	serveCmd.Flags().String("bind", "127.0.0.1", "IP address for the API listener")
	serveCmd.Flags().Int("port", 49375, "Port for the API listener")
	serveCmd.Flags().String("token", "", "Bearer token (or set GHOST_REMOTE_TOKEN)")
	rootCmd.AddCommand(serveCmd)
}
