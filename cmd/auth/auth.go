package auth

import (
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/mdp/qrterminal"
	"github.com/spf13/cobra"
	"golang.org/x/net/publicsuffix"

	"tuya-ipc-terminal/pkg/storage"
	"tuya-ipc-terminal/pkg/tuya"
)

var storageManager *storage.StorageManager

// Available regions
var availableRegions = []tuya.Region{
	{"eu-central", "protect-eu.ismartlife.me", "Central Europe"},
	{"eu-east", "protect-we.ismartlife.me", "East Europe"},
	{"us-west", "protect-us.ismartlife.me", "West America"},
	{"us-east", "protect-ue.ismartlife.me", "East America"},
	{"china", "protect.ismartlife.me", "China"},
	{"india", "protect-in.ismartlife.me", "India"},
}

// SetStorageManager sets the storage manager instance
func SetStorageManager(sm *storage.StorageManager) {
	storageManager = sm
}

// NewAuthCmd creates the auth command
func NewAuthCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "auth",
		Short: "Manage Tuya user authentication",
		Long: `Commands to add, remove, list, and manage Tuya Smart account authentication.

Available Regions:
- eu-central (Central Europe)
- eu-east (East Europe)
- us-west (West America)
- us-east (East America)
- china (China)
- india (India)`,
	}

	cmd.AddCommand(newListCmd())
	cmd.AddCommand(newAddCmd())
	cmd.AddCommand(newRemoveCmd())
	cmd.AddCommand(newRefreshCmd())
	cmd.AddCommand(newTestCmd())

	return cmd
}

// newListCmd creates the list command
func newListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all authenticated users",
		Long:  "Display all stored Tuya Smart account sessions.",
		RunE:  runListUsers,
	}
}

// newAddCmd creates the add command
func newAddCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "add [region] [email]",
		Short: "Add new user authentication",
		Long: `Add a new Tuya Smart account authentication.

Available regions:
  eu-central - Central Europe
  eu-east    - East Europe  
  us-west    - West America
  us-east    - East America
  china      - China
  india      - India

Example:
  tuya-ipc-terminal auth add eu-central user@example.com`,
		Args: cobra.ExactArgs(2),
		RunE: runAddUser,
	}
}

// newRemoveCmd creates the remove command
func newRemoveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "remove [region] [email]",
		Short: "Remove user authentication",
		Long:  "Remove a stored Tuya Smart account session.",
		Args:  cobra.ExactArgs(2),
		RunE:  runRemoveUser,
	}
}

// newRefreshCmd creates the refresh command
func newRefreshCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "refresh [region] [email]",
		Short: "Refresh user session",
		Long:  "Refresh an existing user session by re-authenticating.",
		Args:  cobra.ExactArgs(2),
		RunE:  runRefreshUser,
	}
}

// newTestCmd creates the test command
func newTestCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "test [region] [email]",
		Short: "Test user session validity",
		Long:  "Test if a stored user session is still valid.",
		Args:  cobra.ExactArgs(2),
		RunE:  runTestUser,
	}
}

// runListUsers handles the list command
func runListUsers(cmd *cobra.Command, args []string) error {
	users, err := storageManager.ListUsers()
	if err != nil {
		return fmt.Errorf("failed to list users: %v", err)
	}

	if len(users) == 0 {
		fmt.Println("No authenticated users found.")
		fmt.Println("Use 'tuya-ipc-terminal auth add [region] [email]' to add a user.")
		return nil
	}

	fmt.Printf("Found %d authenticated user(s):\n\n", len(users))

	for i, user := range users {
		status := "✓ Valid"
		if user.SessionData == nil {
			status = "✗ Invalid"
		} else if time.Since(user.LastRefresh) > 7*24*time.Hour {
			status = "⚠ Old (>7 days)"
		}

		fmt.Printf("User %d: %s (%s)\n", i+1, user.Email, user.Region)
		fmt.Printf("  Status: %s\n", status)
		fmt.Printf("  Last refresh: %s\n", user.LastRefresh.Format("2006-01-02 15:04:05"))
		if user.SessionData != nil {
			fmt.Printf("  User ID: %s\n", user.SessionData.LoginResult.Uid)
			fmt.Printf("  Nickname: %s\n", user.SessionData.LoginResult.Nickname)
		}
		fmt.Println()
	}

	return nil
}

// runAddUser handles the add command
func runAddUser(cmd *cobra.Command, args []string) error {
	regionName := args[0]
	email := args[1]

	// Validate region
	var selectedRegion *tuya.Region
	for _, region := range availableRegions {
		if region.Name == regionName {
			selectedRegion = &region
			break
		}
	}

	if selectedRegion == nil {
		fmt.Printf("Invalid region: %s\n", regionName)
		fmt.Println("Available regions:")
		for _, region := range availableRegions {
			fmt.Printf("  %s - %s\n", region.Name, region.Description)
		}

		return fmt.Errorf("invalid region")
	}

	// Validate email format
	if !strings.Contains(email, "@") || !strings.Contains(email, ".") {
		return fmt.Errorf("invalid email format: %s", email)
	}

	// Check if user already exists
	existingUser, err := storageManager.GetUser(regionName, email)
	if err != nil {
		return fmt.Errorf("failed to check existing user: %v", err)
	}

	if existingUser != nil {
		fmt.Printf("User %s in region %s already exists.\n", email, regionName)
		fmt.Println("Do you want to re-authenticate? (y/N): ")

		var response string
		fmt.Scanln(&response)
		if strings.ToLower(response) != "y" && strings.ToLower(response) != "yes" {
			return nil
		}
	}

	fmt.Printf("Adding user %s in region %s (%s)...\n", email, regionName, selectedRegion.Description)

	// Perform authentication
	sessionData, err := performAuthentication(*selectedRegion, email)
	if err != nil {
		return fmt.Errorf("authentication failed: %v", err)
	}

	// Save user session
	if err := storageManager.SaveUser(regionName, email, sessionData); err != nil {
		return fmt.Errorf("failed to save user session: %v", err)
	}

	fmt.Printf("\n✓ Successfully added user %s (%s) in region %s\n",
		sessionData.LoginResult.Nickname, email, regionName)
	fmt.Printf("User ID: %s\n", sessionData.LoginResult.Uid)

	return nil
}

// runRemoveUser handles the remove command
func runRemoveUser(cmd *cobra.Command, args []string) error {
	regionName := args[0]
	email := args[1]

	// Check if user exists
	existingUser, err := storageManager.GetUser(regionName, email)
	if err != nil {
		return fmt.Errorf("failed to check user: %v", err)
	}

	if existingUser == nil {
		return fmt.Errorf("user %s in region %s not found", email, regionName)
	}

	fmt.Printf("Are you sure you want to remove user %s (%s)? (y/N):\n", email, regionName)
	var response string
	fmt.Scanln(&response)
	if strings.ToLower(response) != "y" && strings.ToLower(response) != "yes" {
		fmt.Println("Operation cancelled.")
		return nil
	}

	if err := storageManager.RemoveUser(regionName, email); err != nil {
		return fmt.Errorf("failed to remove user: %v", err)
	}

	fmt.Printf("✓ Successfully removed user %s from region %s\n", email, regionName)
	return nil
}

// runRefreshUser handles the refresh command
func runRefreshUser(cmd *cobra.Command, args []string) error {
	regionName := args[0]
	email := args[1]

	// Validate region
	var selectedRegion *tuya.Region
	for _, region := range availableRegions {
		if region.Name == regionName {
			selectedRegion = &region
			break
		}
	}

	if selectedRegion == nil {
		return fmt.Errorf("invalid region: %s", regionName)
	}

	// Check if user exists
	existingUser, err := storageManager.GetUser(regionName, email)
	if err != nil {
		return fmt.Errorf("failed to check user: %v", err)
	}

	if existingUser == nil {
		return fmt.Errorf("user %s in region %s not found", email, regionName)
	}

	fmt.Printf("Refreshing session for user %s in region %s...\n", email, regionName)

	// Perform authentication
	sessionData, err := performAuthentication(*selectedRegion, email)
	if err != nil {
		return fmt.Errorf("authentication failed: %v", err)
	}

	// Save user session
	if err := storageManager.SaveUser(regionName, email, sessionData); err != nil {
		return fmt.Errorf("failed to save user session: %v", err)
	}

	fmt.Printf("✓ Successfully refreshed session for user %s (%s)\n",
		sessionData.LoginResult.Nickname, email)

	return nil
}

// runTestUser handles the test command
func runTestUser(cmd *cobra.Command, args []string) error {
	regionName := args[0]
	email := args[1]

	user, err := storageManager.GetUser(regionName, email)
	if err != nil {
		return fmt.Errorf("failed to get user: %v", err)
	}

	if user == nil {
		fmt.Printf("✗ User %s in region %s not found\n", email, regionName)
		return nil
	}

	if user.SessionData == nil {
		fmt.Printf("✗ User %s has invalid session data\n", email)
		return nil
	}

	// Test session validity by making an API call
	httpClient := createHTTPClientWithSession(user.SessionData)
	if httpClient == nil {
		fmt.Printf("✗ Failed to create HTTP client for user %s\n", email)
		return nil
	}

	fmt.Printf("Testing session for %s (%s)...", email, regionName)

	_, err = tuya.GetAppInfo(httpClient, user.SessionData.ServerHost)
	if err != nil {
		fmt.Printf("✗ Session is invalid: %v\n", err)
		fmt.Println("Try refreshing the session with:")
		fmt.Printf("  tuya-ipc-terminal auth refresh %s %s\n", regionName, email)
		return nil
	}

	fmt.Printf("✓ Session is valid for user %s (%s)\n", user.SessionData.LoginResult.Nickname, email)
	fmt.Printf("User ID: %s\n", user.SessionData.LoginResult.Uid)
	fmt.Printf("Last refresh: %s\n", user.LastRefresh.Format("2006-01-02 15:04:05"))

	return nil
}

// performAuthentication performs the QR code authentication flow
func performAuthentication(region tuya.Region, email string) (*tuya.SessionData, error) {
	serverHost := region.Host

	// Create HTTP client
	httpClient := createHTTPClientWithSession(nil)

	// Generate QR code
	fmt.Println("Generating QR code...")
	qrCodeToken, err := tuya.GenerateQRCode(httpClient, serverHost)
	if err != nil {
		return nil, fmt.Errorf("error generating QR code: %v", err)
	}

	// Show QR code
	qrterminal.Generate("tuyaSmart--qrLogin?token="+qrCodeToken, qrterminal.L, os.Stdout)
	fmt.Printf("\nPlease scan the QR code with the Tuya Smart / Smart Life app.\n")
	fmt.Printf("Make sure to use the account with email: %s\n", email)
	fmt.Println("\nPress Enter after scanning to continue...")
	fmt.Scanln()

	// Poll for login status
	fmt.Println("Polling for login status...")
	loginResult, err := tuya.PollForLogin(httpClient, serverHost, qrCodeToken)
	if err != nil {
		return nil, fmt.Errorf("error polling for login: %v", err)
	}

	// Check if logged in email matches expected
	if loginResult.Email != email {
		fmt.Println("Logged in with different email than expected!")
		fmt.Printf("Expected: %s, Got: %s\n", email, loginResult.Email)
		fmt.Println("Continue anyway? (y/N): ")
		var response string
		fmt.Scanln(&response)
		if strings.ToLower(response) != "y" && strings.ToLower(response) != "yes" {
			return nil, fmt.Errorf("email mismatch, authentication cancelled")
		}
	}

	// Create session data
	sessionData := &tuya.SessionData{
		LoginResult:   loginResult,
		Cookies:       extractCookies(httpClient, serverHost),
		LastValidated: time.Now(),
		ServerHost:    serverHost,
		Region:        region.Name,
		UserEmail:     loginResult.Email,
	}

	return sessionData, nil
}

// createHTTPClientWithSession creates an HTTP client with session cookies
func createHTTPClientWithSession(session *tuya.SessionData) *http.Client {
	jar, err := cookiejar.New(&cookiejar.Options{
		PublicSuffixList: publicsuffix.List,
	})
	if err != nil {
		return nil
	}

	if session != nil && len(session.Cookies) > 0 {
		serverURL, _ := url.Parse(fmt.Sprintf("https://%s", session.ServerHost))

		var httpCookies []*http.Cookie
		for _, cookie := range session.Cookies {
			httpCookies = append(httpCookies, &http.Cookie{
				Name:     cookie.Name,
				Value:    cookie.Value,
				Domain:   cookie.Domain,
				Path:     cookie.Path,
				Expires:  cookie.Expires,
				Secure:   cookie.Secure,
				HttpOnly: cookie.HttpOnly,
			})
		}

		jar.SetCookies(serverURL, httpCookies)
	}

	return &http.Client{
		Timeout: 30 * time.Second,
		Jar:     jar,
	}
}

// extractCookies extracts cookies from HTTP client for storage
func extractCookies(client *http.Client, serverHost string) []*tuya.Cookie {
	var cookies []*tuya.Cookie
	if client.Jar != nil {
		serverURL, _ := url.Parse(fmt.Sprintf("https://%s", serverHost))
		httpCookies := client.Jar.Cookies(serverURL)

		for _, httpCookie := range httpCookies {
			cookies = append(cookies, &tuya.Cookie{
				Name:     httpCookie.Name,
				Value:    httpCookie.Value,
				Domain:   httpCookie.Domain,
				Path:     httpCookie.Path,
				Expires:  httpCookie.Expires,
				Secure:   httpCookie.Secure,
				HttpOnly: httpCookie.HttpOnly,
			})
		}
	}

	return cookies
}
