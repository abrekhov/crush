package cmd

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"time"

	"github.com/abrekhov/crush/internal/config"
	crushlog "github.com/abrekhov/crush/internal/log"
	"github.com/abrekhov/crush/internal/server"
	"github.com/charmbracelet/x/term"
	"github.com/spf13/cobra"
)

var serverHost string

func init() {
	serverCmd.Flags().StringVarP(&serverHost, "host", "H", server.DefaultHost(), "Server host (TCP or Unix socket)")
	rootCmd.AddCommand(serverCmd)
}

var serverCmd = &cobra.Command{
	Use:   "server",
	Short: "Start the Crush server",
	Long: `Start the Crush server.

When run in an interactive terminal the server also launches a full TUI
session. Quitting the TUI leaves the server running; attach again with
'crush attach'. Stop the server with Ctrl+C.`,
	RunE: func(cmd *cobra.Command, _ []string) error {
		dataDir, err := cmd.Flags().GetString("data-dir")
		if err != nil {
			return fmt.Errorf("failed to get data directory: %v", err)
		}
		debug, err := cmd.Flags().GetBool("debug")
		if err != nil {
			return fmt.Errorf("failed to get debug flag: %v", err)
		}

		cfg, err := config.Load(config.GlobalWorkspaceDir(), dataDir, debug)
		if err != nil {
			return fmt.Errorf("failed to load configuration: %v", err)
		}

		logFile := filepath.Join(config.GlobalCacheDir(), "server-"+safeNameRegexp.ReplaceAllString(serverHost, "_"), "crush.log")

		if term.IsTerminal(os.Stderr.Fd()) {
			crushlog.Setup(logFile, debug, os.Stderr)
		} else {
			crushlog.Setup(logFile, debug)
		}

		hostURL, err := server.ParseHostURL(serverHost)
		if err != nil {
			return fmt.Errorf("invalid server host: %v", err)
		}

		srv := server.NewServer(cfg, hostURL.Scheme, hostURL.Host)
		srv.SetLogger(slog.Default())
		slog.Info("Starting Crush server...", "addr", serverHost)

		errch := make(chan error, 1)
		sigch := make(chan os.Signal, 1)
		sigs := []os.Signal{os.Interrupt}
		sigs = append(sigs, addSignals(sigs)...)
		signal.Notify(sigch, sigs...)

		go func() {
			errch <- srv.ListenAndServe()
		}()

		// When running in an interactive terminal, launch a TUI that connects
		// back to this server. Quitting the TUI returns here and the server
		// continues running headlessly until a signal or error.
		if term.IsTerminal(os.Stdin.Fd()) && term.IsTerminal(os.Stdout.Fd()) {
			if waitErr := waitForSocket(cmd, hostURL); waitErr != nil {
				_ = srv.Close()
				return waitErr
			}

			ws, wsCleanup, attachErr := setupAttachWorkspace(cmd, hostURL)
			if attachErr != nil {
				slog.Warn("Could not attach TUI to server, running headlessly", "error", attachErr)
			} else {
				_ = runTUI(cmd, ws, "", false)
				wsCleanup()
				slog.Info("TUI exited, server continues running. Use 'crush attach' to reconnect.")
			}
		}

		// Headless: wait for signal or server error.
		select {
		case <-sigch:
			slog.Info("Received interrupt signal...")
		case err = <-errch:
			if err != nil && !errors.Is(err, server.ErrServerClosed) {
				_ = srv.Close()
				slog.Error("Server error", "error", err)
				return fmt.Errorf("server error: %v", err)
			}
		}

		if errors.Is(err, server.ErrServerClosed) {
			return nil
		}

		ctx, cancel := context.WithTimeout(cmd.Context(), 5*time.Second)
		defer cancel()

		slog.Info("Shutting down...")

		if err := srv.Shutdown(ctx); err != nil {
			slog.Error("Failed to shutdown server", "error", err)
			return fmt.Errorf("failed to shutdown server: %v", err)
		}

		return nil
	},
}

// waitForSocket polls until the Unix socket (or named pipe) at hostURL is
// ready, returning an error after 10 attempts (~1 second).
func waitForSocket(cmd *cobra.Command, hostURL *url.URL) error {
	switch hostURL.Scheme {
	case "unix", "npipe":
		var statErr error
		for range 10 {
			_, statErr = os.Stat(hostURL.Host)
			if statErr == nil {
				return nil
			}
			select {
			case <-cmd.Context().Done():
				return cmd.Context().Err()
			case <-time.After(100 * time.Millisecond):
			}
		}
		return fmt.Errorf("server socket not ready: %v", statErr)
	}
	return nil
}

func init() {
	attachCmd.Flags().StringP("session", "s", "", "Continue a previous session by ID")
	attachCmd.Flags().BoolP("continue", "C", false, "Continue the most recent session")
	attachCmd.MarkFlagsMutuallyExclusive("session", "continue")
	rootCmd.AddCommand(attachCmd)
}

var attachCmd = &cobra.Command{
	Use:   "attach",
	Short: "Attach a TUI to a running Crush server",
	Long: `Connect to an already-running Crush server and open an interactive TUI
session. Fails immediately if no server is running at the specified host.

Start a server first with 'crush server'.`,
	RunE: func(cmd *cobra.Command, _ []string) error {
		hostURL, err := server.ParseHostURL(clientHost)
		if err != nil {
			return fmt.Errorf("invalid host: %v", err)
		}

		// For socket-based transports, verify the server is actually running
		// before attempting to connect so the error is clear.
		if hostURL.Scheme == "unix" || hostURL.Scheme == "npipe" {
			if _, err := os.Stat(hostURL.Host); err != nil {
				return fmt.Errorf("no server running at %s (start one with 'crush server')", hostURL.Host)
			}
		}

		ws, wsCleanup, err := setupAttachWorkspace(cmd, hostURL)
		if err != nil {
			return fmt.Errorf("failed to connect to server: %v", err)
		}
		defer wsCleanup()

		sessionID, _ := cmd.Flags().GetString("session")
		continueLast, _ := cmd.Flags().GetBool("continue")

		if sessionID != "" {
			sess, err := resolveWorkspaceSessionID(cmd.Context(), ws, sessionID)
			if err != nil {
				return err
			}
			sessionID = sess.ID
		}

		return runTUI(cmd, ws, sessionID, continueLast)
	},
}
