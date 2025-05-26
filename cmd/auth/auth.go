package auth

import (
	"bufio"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"strings"
	"syscall"
	"time"

	"github.com/mdp/qrterminal"
	"github.com/spf13/cobra"
	"golang.org/x/net/publicsuffix"
	"golang.org/x/term"

	"tuya-ipc-terminal/pkg/storage"
	"tuya-ipc-terminal/pkg/tuya"
)

var storageManager *storage.StorageManager

func SetStorageManager(sm *storage.StorageManager) {
	storageManager = sm
}

func NewAuthCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "auth",
		Short: "Manage Tuya user authentication",
		Long:  "Commands to add, remove, list, and manage Tuya Smart account authentication",
	}

	cmd.AddCommand(newListCmd())
	cmd.AddCommand(newAddCmd())
	cmd.AddCommand(newRemoveCmd())
	cmd.AddCommand(newRefreshCmd())
	cmd.AddCommand(newTestCmd())
	cmd.AddCommand(newShowRegionsCmd())
	cmd.AddCommand(newShowCountryCodesCmd())

	return cmd
}

func newListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all authenticated users",
		Long:  "Display all stored Tuya Smart account sessions.",
		RunE:  runListUsers,
	}
}

func newAddCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "add [region] [email]",
		Short: "Add new user authentication",
		Long: `Add a new Tuya Smart account authentication.

Authentication methods:
  --qr       Use QR code authentication (default)
  --password Use email/password authentication

Example:
  tuya-ipc-terminal auth add eu-central user@example.com
  tuya-ipc-terminal auth add --password eu-central user@example.com`,
		Args: cobra.ExactArgs(2),
		RunE: runAddUser,
	}

	cmd.Flags().Bool("qr", false, "Use QR code authentication (default)")
	cmd.Flags().Bool("password", false, "Use email/password authentication")

	return cmd
}

func newRemoveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "remove [region] [email]",
		Short: "Remove user authentication",
		Long:  "Remove a stored Tuya Smart account session.",
		Args:  cobra.ExactArgs(2),
		RunE:  runRemoveUser,
	}
}

func newRefreshCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "refresh [region] [email]",
		Short: "Refresh user session",
		Long:  "Refresh an existing user session by re-authenticating.",
		Args:  cobra.ExactArgs(2),
		RunE:  runRefreshUser,
	}

	cmd.Flags().Bool("qr", false, "Use QR code authentication (default)")
	cmd.Flags().Bool("password", false, "Use email/password authentication")

	return cmd
}

func newTestCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "test [region] [email]",
		Short: "Test user session validity",
		Long:  "Test if a stored user session is still valid.",
		Args:  cobra.ExactArgs(2),
		RunE:  runTestUser,
	}
}

func newShowRegionsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show-regions",
		Short: "Show all available regions",
		Long:  "Display all available Tuya Smart regions with their endpoints and descriptions.",
		RunE:  runShowRegions,
	}
}

func newShowCountryCodesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "show-country-codes",
		Short: "Show country codes",
		Long:  "Display country codes for phone number authentication.",
		RunE:  runShowCountryCodes,
	}

	cmd.Flags().StringP("search", "s", "", "Search for countries by name")

	return cmd
}

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

func runAddUser(cmd *cobra.Command, args []string) error {
	regionName := args[0]
	email := args[1]

	usePassword, _ := cmd.Flags().GetBool("password")
	useQR, _ := cmd.Flags().GetBool("qr")

	if !usePassword && !useQR {
		authMethod := promptAuthMethod()
		usePassword = (authMethod == "password")
	}

	var selectedRegion *tuya.Region
	for _, region := range AvailableRegions {
		if region.Name == regionName {
			selectedRegion = &region
			break
		}
	}

	if selectedRegion == nil {
		fmt.Printf("Invalid region: %s\n", regionName)
		fmt.Println("Available regions:")
		for _, region := range AvailableRegions {
			fmt.Printf("  %s - %s\n", region.Name, region.Description)
		}
		return fmt.Errorf("invalid region")
	}

	if !strings.Contains(email, "@") || !strings.Contains(email, ".") {
		return fmt.Errorf("invalid email format: %s", email)
	}

	existingUser, err := storageManager.GetUser(selectedRegion.Name, email)
	if err != nil {
		return fmt.Errorf("failed to check existing user: %v", err)
	}

	if existingUser != nil {
		fmt.Printf("User %s in region %s already exists.\n", email, selectedRegion.Name)
		fmt.Println("Do you want to re-authenticate? (y/N): ")

		var response string
		fmt.Scanln(&response)
		if strings.ToLower(response) != "y" && strings.ToLower(response) != "yes" {
			return nil
		}
	}

	authMethodStr := "QR code"
	if usePassword {
		authMethodStr = "email/password"
	}

	fmt.Println()
	fmt.Printf("Adding user %s in region %s (%s) using %s authentication...\n",
		email, selectedRegion.Name, selectedRegion.Description, authMethodStr)

	var sessionData *tuya.SessionData
	if usePassword {
		sessionData, err = performPasswordAuthentication(*selectedRegion, email)
	} else {
		sessionData, err = performQRAuthentication(*selectedRegion, email)
	}

	if err != nil {
		return fmt.Errorf("authentication failed: %v", err)
	}

	if err := storageManager.SaveUser(selectedRegion.Name, email, sessionData); err != nil {
		return fmt.Errorf("failed to save user session: %v", err)
	}

	fmt.Printf("\n✓ Successfully added user %s (%s) in region %s\n",
		sessionData.LoginResult.Nickname, email, selectedRegion.Name)
	fmt.Printf("User ID: %s\n", sessionData.LoginResult.Uid)

	return nil
}

func runRemoveUser(cmd *cobra.Command, args []string) error {
	regionName := args[0]
	email := args[1]

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

func runRefreshUser(cmd *cobra.Command, args []string) error {
	regionName := args[0]
	email := args[1]

	usePassword, _ := cmd.Flags().GetBool("password")
	useQR, _ := cmd.Flags().GetBool("qr")

	if !usePassword && !useQR {
		authMethod := promptAuthMethod()
		usePassword = (authMethod == "password")
	}

	var selectedRegion *tuya.Region
	for _, region := range AvailableRegions {
		if region.Name == regionName {
			selectedRegion = &region
			break
		}
	}

	if selectedRegion == nil {
		return fmt.Errorf("invalid region: %s", regionName)
	}

	existingUser, err := storageManager.GetUser(regionName, email)
	if err != nil {
		return fmt.Errorf("failed to check user: %v", err)
	}

	if existingUser == nil {
		return fmt.Errorf("user %s in region %s not found", email, regionName)
	}

	authMethodStr := "QR code"
	if usePassword {
		authMethodStr = "email/password"
	}

	fmt.Printf("Refreshing session for user %s in region %s using %s authentication...\n",
		email, regionName, authMethodStr)

	var sessionData *tuya.SessionData
	if usePassword {
		sessionData, err = performPasswordAuthentication(*selectedRegion, email)
	} else {
		sessionData, err = performQRAuthentication(*selectedRegion, email)
	}

	if err != nil {
		return fmt.Errorf("authentication failed: %v", err)
	}

	if err := storageManager.SaveUser(regionName, email, sessionData); err != nil {
		return fmt.Errorf("failed to save user session: %v", err)
	}

	fmt.Printf("✓ Successfully refreshed session for user %s (%s)\n",
		sessionData.LoginResult.Nickname, email)

	return nil
}

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

	httpClient := createHTTPClientWithSession(user.SessionData)
	if httpClient == nil {
		fmt.Printf("✗ Failed to create HTTP client for user %s\n", email)
		return nil
	}

	fmt.Printf("Testing session for %s (%s)..\n", email, regionName)

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

func runShowRegions(cmd *cobra.Command, args []string) error {
	fmt.Println(strings.Repeat("-", 70))
	fmt.Printf("%-15s %-35s %s\n", "REGION", "ENDPOINT", "DESCRIPTION")
	fmt.Println(strings.Repeat("-", 70))

	for _, region := range AvailableRegions {
		fmt.Printf("%-15s %-35s %s\n", region.Name, region.Host, region.Description)
	}

	fmt.Println(strings.Repeat("-", 70))
	fmt.Printf("Total: %d regions available\n", len(AvailableRegions))
	fmt.Println("\nUsage examples:")
	fmt.Println("  tuya-ipc-terminal auth add eu-central user@example.com")
	fmt.Println("  tuya-ipc-terminal auth add --password us-west user@example.com")

	return nil
}

func runShowCountryCodes(cmd *cobra.Command, args []string) error {
	search, _ := cmd.Flags().GetString("search")

	var filteredCountries []CountryCode
	for _, country := range CountryCodesData {
		if search != "" {
			searchLower := strings.ToLower(search)
			countryName := strings.ToLower(country.N)
			if !strings.Contains(countryName, searchLower) && country.C != search {
				continue
			}
		}

		filteredCountries = append(filteredCountries, country)
	}

	fmt.Println(strings.Repeat("-", 70))
	fmt.Printf("Country Codes")

	if search != "" {
		fmt.Printf(" (search: '%s')", search)
	}

	fmt.Println(":")
	fmt.Println(strings.Repeat("-", 70))
	fmt.Printf("%-8s %-3s %-25s %s\n", "CODE", "ISO", "COUNTRY", "CONTINENT")
	fmt.Println(strings.Repeat("-", 70))

	displayLimit := 50

	for i, country := range filteredCountries {
		if i >= displayLimit {
			fmt.Printf("... and %d more countries\n", len(filteredCountries)-displayLimit)
			fmt.Println("\nUse filters to narrow results:")
			fmt.Println("  --search \"germany\"     Search by name")
			break
		}

		countryName := country.N
		if len(countryName) > 25 {
			countryName = countryName[:22] + "..."
		}

		fmt.Printf("%-8s %-3s %-25s %s\n",
			country.C,
			country.A,
			countryName,
			country.Continent)
	}

	fmt.Println(strings.Repeat("-", 70))
	fmt.Printf("Total: %d countries", len(filteredCountries))
	if len(filteredCountries) != len(CountryCodesData) {
		fmt.Printf(" (filtered from %d)", len(CountryCodesData))
	}
	fmt.Println()

	if search == "" {
		fmt.Println("\nTip: Use filters to find countries faster:")
		fmt.Println("  tuya-ipc-terminal auth show-country-codes --search germany")
	}

	fmt.Println("\nUsage in authentication:")
	fmt.Println("  tuya-ipc-terminal auth add --password eu-central user@example.com")
	fmt.Println("  (You'll be prompted to select your country code)")

	return nil
}

func promptAuthMethod() string {
	fmt.Println("Choose authentication method:")
	fmt.Println("1. QR Code (default)")
	fmt.Println("2. Email/Password")
	fmt.Print("Enter choice (1-2): ")

	reader := bufio.NewReader(os.Stdin)
	choice, _ := reader.ReadString('\n')
	choice = strings.TrimSpace(choice)

	switch choice {
	case "2":
		return "password"
	default:
		return "qr"
	}
}

func promptPassword() (string, error) {
	fmt.Print("Enter password: ")

	password, err := term.ReadPassword(int(syscall.Stdin))
	if err != nil {
		return "", fmt.Errorf("failed to read password: %v", err)
	}

	return string(password), nil
}

func performPasswordAuthentication(region tuya.Region, email string) (*tuya.SessionData, error) {
	serverHost := region.Host

	password, err := promptPassword()
	if err != nil {
		return nil, fmt.Errorf("failed to get password: %v", err)
	}

	httpClient := createHTTPClientWithSession(nil)

	fmt.Println("\nAuthenticating with email/password...")

	loginResult, err := tuya.PasswordLogin(httpClient, serverHost, email, password, region.Continent)
	if err != nil {
		return nil, fmt.Errorf("password authentication failed: %v", err)
	}

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

func performQRAuthentication(region tuya.Region, email string) (*tuya.SessionData, error) {
	serverHost := region.Host

	httpClient := createHTTPClientWithSession(nil)

	fmt.Println("Generating QR code...")
	qrCodeToken, err := tuya.GenerateQRCode(httpClient, serverHost)
	if err != nil {
		return nil, fmt.Errorf("error generating QR code: %v", err)
	}

	qrterminal.Generate("tuyaSmart--qrLogin?token="+qrCodeToken, qrterminal.L, os.Stdout)
	fmt.Printf("\nPlease scan the QR code with the Tuya Smart / Smart Life app.\n")
	fmt.Printf("Make sure to use the account with email: %s\n", email)
	fmt.Println("\nPress Enter after scanning to continue...")
	fmt.Scanln()

	fmt.Println("Polling for login status...")
	loginResult, err := tuya.PollForLogin(httpClient, serverHost, qrCodeToken)
	if err != nil {
		return nil, fmt.Errorf("error polling for login: %v", err)
	}

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
