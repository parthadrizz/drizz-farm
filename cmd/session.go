package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/drizz-dev/drizz-farm/internal/session"
)

var (
	sessionProfile    string
	sessionPlatform   string
	sessionTimeoutMin int
	sessionID         string
)

var sessionCmd = &cobra.Command{
	Use:   "session",
	Short: "Manage emulator sessions",
}

var sessionCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a new emulator session",
	RunE:  runSessionCreate,
}

var sessionListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all sessions",
	RunE:  runSessionList,
}

var sessionReleaseCmd = &cobra.Command{
	Use:   "release",
	Short: "Release an active session",
	RunE:  runSessionRelease,
}

func init() {
	sessionCreateCmd.Flags().StringVarP(&sessionProfile, "profile", "p", "", "emulator profile (e.g., pixel_7_api34)")
	sessionCreateCmd.Flags().StringVar(&sessionPlatform, "platform", "android", "platform (android or ios)")
	sessionCreateCmd.Flags().IntVarP(&sessionTimeoutMin, "timeout", "t", 0, "session timeout in minutes")
	_ = sessionCreateCmd.MarkFlagRequired("profile")

	sessionReleaseCmd.Flags().StringVar(&sessionID, "id", "", "session ID to release")
	_ = sessionReleaseCmd.MarkFlagRequired("id")

	sessionCmd.AddCommand(sessionCreateCmd)
	sessionCmd.AddCommand(sessionListCmd)
	sessionCmd.AddCommand(sessionReleaseCmd)

	// Register under "drizz-farm session" and also "drizz-farm farm session"
	rootCmd.AddCommand(sessionCmd)
}

func runSessionCreate(cmd *cobra.Command, args []string) error {
	req := session.CreateSessionRequest{
		Platform:   sessionPlatform,
		Profile:    sessionProfile,
		Source:     "cli",
		TimeoutMin: sessionTimeoutMin,
	}

	body, _ := json.Marshal(req)
	resp, err := http.Post("http://127.0.0.1:9401/api/v1/sessions", "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("connect to daemon: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusCreated {
		fmt.Printf("Error: %s\n", string(respBody))
		return fmt.Errorf("session create failed (status %d)", resp.StatusCode)
	}

	if jsonOut {
		fmt.Println(string(respBody))
		return nil
	}

	var sess session.Session
	if err := json.Unmarshal(respBody, &sess); err != nil {
		return fmt.Errorf("parse response: %w", err)
	}

	fmt.Printf("Session created!\n\n")
	fmt.Printf("  ID:       %s\n", sess.ID)
	fmt.Printf("  Node:     %s\n", sess.NodeName)
	fmt.Printf("  Profile:  %s\n", sess.Profile)
	fmt.Printf("  Serial:   %s\n", sess.Connection.ADBSerial)
	fmt.Printf("  ADB:      %s:%d\n", sess.Connection.Host, sess.Connection.ADBPort)
	fmt.Printf("  Console:  %s:%d\n", sess.Connection.Host, sess.Connection.ConsolePort)
	fmt.Printf("  Expires:  %s\n\n", sess.ExpiresAt.Format(time.RFC3339))
	fmt.Printf("Connect with:\n")
	fmt.Printf("  adb connect %s:%d\n\n", sess.Connection.Host, sess.Connection.ADBPort)
	fmt.Printf("Release when done:\n")
	fmt.Printf("  drizz-farm session release --id %s\n", sess.ID)

	return nil
}

func runSessionList(cmd *cobra.Command, args []string) error {
	resp, err := http.Get("http://127.0.0.1:9401/api/v1/sessions")
	if err != nil {
		return fmt.Errorf("connect to daemon: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	if jsonOut {
		fmt.Println(string(body))
		return nil
	}

	var result struct {
		Sessions []*session.Session `json:"sessions"`
		Active   int                `json:"active"`
		Queued   int                `json:"queued"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return fmt.Errorf("parse response: %w", err)
	}

	fmt.Printf("Sessions (active: %d, queued: %d)\n\n", result.Active, result.Queued)

	if len(result.Sessions) == 0 {
		fmt.Println("No sessions.")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tNODE\tPROFILE\tSTATE\tSERIAL\tADB\tCREATED\tEXPIRES")
	for _, sess := range result.Sessions {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s:%d\t%s\t%s\n",
			sess.ID,
			sess.NodeName,
			sess.Profile,
			sess.State,
			sess.Connection.ADBSerial,
			sess.Connection.Host,
			sess.Connection.ADBPort,
			sess.CreatedAt.Format("15:04:05"),
			sess.ExpiresAt.Format("15:04:05"),
		)
	}
	w.Flush()

	return nil
}

func runSessionRelease(cmd *cobra.Command, args []string) error {
	url := fmt.Sprintf("http://127.0.0.1:9401/api/v1/sessions/%s", sessionID)
	req, _ := http.NewRequest(http.MethodDelete, url, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("connect to daemon: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		fmt.Printf("Error: %s\n", string(body))
		return fmt.Errorf("release failed (status %d)", resp.StatusCode)
	}

	fmt.Printf("Session %s released.\n", sessionID)
	return nil
}
