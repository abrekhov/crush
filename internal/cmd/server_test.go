package cmd

import (
	"context"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

// fakeCmd returns a minimal cobra.Command with the persistent flags that
// waitForSocket and attachCmd read (debug, data-dir, host).
func fakeCmd(t *testing.T) *cobra.Command {
	t.Helper()
	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())
	cmd.PersistentFlags().Bool("debug", false, "")
	cmd.PersistentFlags().String("data-dir", "", "")
	cmd.PersistentFlags().StringVarP(&clientHost, "host", "H", "", "")
	cmd.Flags().Bool("yolo", false, "")
	cmd.Flags().String("cwd", "", "")
	return cmd
}

func TestWaitForSocket_ExistingSocket(t *testing.T) {
	dir := t.TempDir()
	sockPath := filepath.Join(dir, "crush.sock")

	// Create the socket file before calling waitForSocket.
	f, err := os.Create(sockPath)
	require.NoError(t, err)
	f.Close()

	hostURL := &url.URL{Scheme: "unix", Host: sockPath}
	cmd := fakeCmd(t)

	start := time.Now()
	err = waitForSocket(cmd, hostURL)
	require.NoError(t, err)
	// Should return almost immediately (no polling needed).
	require.Less(t, time.Since(start), 500*time.Millisecond)
}

func TestWaitForSocket_Timeout(t *testing.T) {
	dir := t.TempDir()
	sockPath := filepath.Join(dir, "nonexistent.sock")

	hostURL := &url.URL{Scheme: "unix", Host: sockPath}
	cmd := fakeCmd(t)

	err := waitForSocket(cmd, hostURL)
	require.Error(t, err)
	require.Contains(t, err.Error(), "server socket not ready")
}

func TestWaitForSocket_ContextCancelled(t *testing.T) {
	dir := t.TempDir()
	sockPath := filepath.Join(dir, "nonexistent.sock")

	hostURL := &url.URL{Scheme: "unix", Host: sockPath}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled

	cmd := &cobra.Command{}
	cmd.SetContext(ctx)

	err := waitForSocket(cmd, hostURL)
	require.ErrorIs(t, err, context.Canceled)
}

func TestWaitForSocket_TCPSkipped(t *testing.T) {
	// For TCP hosts waitForSocket does nothing (returns nil immediately).
	hostURL := &url.URL{Scheme: "tcp", Host: "localhost:9999"}
	cmd := fakeCmd(t)

	err := waitForSocket(cmd, hostURL)
	require.NoError(t, err)
}

func TestAttachCmd_NoServerRunning(t *testing.T) {
	dir := t.TempDir()
	sockPath := filepath.Join(dir, "nonexistent.sock")

	// Point clientHost at a unix socket URL that does not exist.
	clientHost = "unix://" + sockPath

	var errOutput strings.Builder
	attachCmd.SetErr(&errOutput)
	attachCmd.SetOut(&errOutput)

	err := attachCmd.RunE(attachCmd, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "no server running at")
	require.Contains(t, err.Error(), "crush server")
}
