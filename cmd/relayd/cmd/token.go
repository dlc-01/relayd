package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

var tokenCmd = &cobra.Command{
	Use:   "token",
	Short: "Manage tokens",
}

var tokenIssueCmd = &cobra.Command{
	Use:   "issue",
	Short: "Issue a new temporary token",
	RunE:  runTokenIssue,
}

var tokenListCmd = &cobra.Command{
	Use:   "list",
	Short: "List active tokens",
	RunE:  runTokenList,
}

var tokenRevokeCmd = &cobra.Command{
	Use:   "revoke <token>",
	Short: "Revoke a token",
	Args:  cobra.ExactArgs(1),
	RunE:  runTokenRevoke,
}

func init() {
	tokenCmd.AddCommand(tokenIssueCmd)
	tokenCmd.AddCommand(tokenListCmd)
	tokenCmd.AddCommand(tokenRevokeCmd)

	tokenIssueCmd.Flags().String("ttl", "24h", "Token TTL e.g. 2h, 24h, 7d")
	tokenIssueCmd.Flags().String("label", "", "Token label e.g. vasya")
	tokenIssueCmd.Flags().String("admin", "http://localhost:7002", "Admin API address")
	tokenIssueCmd.Flags().String("master", "", "Master token")
	tokenIssueCmd.MarkFlagRequired("master")
	tokenIssueCmd.MarkFlagRequired("label")

	tokenListCmd.Flags().String("admin", "http://localhost:7002", "Admin API address")
	tokenListCmd.Flags().String("master", "", "Master token")
	tokenListCmd.MarkFlagRequired("master")

	tokenRevokeCmd.Flags().String("admin", "http://localhost:7002", "Admin API address")
	tokenRevokeCmd.Flags().String("master", "", "Master token")
	tokenRevokeCmd.MarkFlagRequired("master")
}

func doAdminRequest(method, url, masterToken string, body io.Reader) (*http.Response, error) {
	client := &http.Client{Timeout: 5 * time.Second}
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+masterToken)
	req.Header.Set("Content-Type", "application/json")
	return client.Do(req)
}

func runTokenIssue(cmd *cobra.Command, args []string) error {
	admin, _ := cmd.Flags().GetString("admin")
	master, _ := cmd.Flags().GetString("master")
	ttl, _ := cmd.Flags().GetString("ttl")
	label, _ := cmd.Flags().GetString("label")

	body := fmt.Sprintf(`{"ttl":%q,"label":%q}`, ttl, label)
	resp, err := doAdminRequest(http.MethodPost, admin+"/token/issue", master, strings.NewReader(body))
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		data, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("server error %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}

	var result map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("decode: %w", err)
	}

	fmt.Printf("token:      %s\n", result["token"])
	fmt.Printf("label:      %s\n", result["label"])
	fmt.Printf("expires_at: %s\n", result["expires_at"])
	return nil
}

func runTokenList(cmd *cobra.Command, args []string) error {
	admin, _ := cmd.Flags().GetString("admin")
	master, _ := cmd.Flags().GetString("master")

	resp, err := doAdminRequest(http.MethodGet, admin+"/token/list", master, nil)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		data, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("server error %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}

	var result []map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("decode: %w", err)
	}

	if len(result) == 0 {
		fmt.Println("no active tokens")
		return nil
	}

	fmt.Printf("%-36s  %-20s  %s\n", "TOKEN", "LABEL", "EXPIRES_AT")
	fmt.Println(strings.Repeat("-", 80))
	for _, t := range result {
		fmt.Printf("%-36s  %-20s  %s\n", t["token"], t["label"], t["expires_at"])
	}
	return nil
}

func runTokenRevoke(cmd *cobra.Command, args []string) error {
	admin, _ := cmd.Flags().GetString("admin")
	master, _ := cmd.Flags().GetString("master")
	token := args[0]

	body := fmt.Sprintf(`{"token":%q}`, token)
	resp, err := doAdminRequest(http.MethodDelete, admin+"/token/revoke", master, strings.NewReader(body))
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		data, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("server error %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}

	fmt.Printf("token %s revoked\n", token)
	return nil
}
