package cameras

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strconv"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/net/publicsuffix"

	"tuya-ipc-terminal/pkg/storage"
	"tuya-ipc-terminal/pkg/tuya"
)

var storageManager *storage.StorageManager

func SetStorageManager(sm *storage.StorageManager) {
	storageManager = sm
}

func NewCamerasCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cameras",
		Short: "Manage camera discovery and information",
		Long:  "Commands to discover, list, and manage Tuya Smart cameras.",
	}

	cmd.AddCommand(newListCmd())
	cmd.AddCommand(newRefreshCmd())
	cmd.AddCommand(newInfoCmd())

	return cmd
}

func newListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List all discovered cameras",
		Long:  "Display all cameras discovered from authenticated users.",
		RunE:  runListCameras,
	}

	cmd.Flags().StringP("user", "u", "", "Filter by specific user (format: region_email)")
	cmd.Flags().BoolP("online-only", "o", false, "Show only online cameras")

	return cmd
}

func newRefreshCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "refresh",
		Short: "Refresh camera discovery",
		Long:  "Rediscover cameras from all authenticated users.",
		RunE:  runRefreshCameras,
	}
}

func newInfoCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "info [camera-id]",
		Short: "Show detailed camera information",
		Long:  "Display detailed information about a specific camera.",
		Args:  cobra.ExactArgs(1),
		RunE:  runCameraInfo,
	}
}

func runListCameras(cmd *cobra.Command, args []string) error {
	userFilter, _ := cmd.Flags().GetString("user")

	var cameras []storage.CameraInfo
	var err error

	if userFilter != "" {
		cameras, err = storageManager.GetCamerasForUser(userFilter)
	} else {
		cameras, err = storageManager.GetAllCameras()
	}

	if err != nil {
		return fmt.Errorf("failed to get cameras: %v", err)
	}

	if len(cameras) == 0 {
		if userFilter != "" {
			fmt.Printf("No cameras found for user: %s\n", userFilter)
		} else {
			fmt.Println("No cameras found.")
			fmt.Println("Use 'tuya-ipc-terminal cameras refresh' to discover cameras.")
		}
		return nil
	}

	fmt.Printf("Found %d camera(s):\n\n", len(cameras))

	for i, cam := range cameras {
		fmt.Printf("%d. %s (%s)\n", i+1, cam.DeviceName, cam.DeviceID)
		fmt.Printf("   User: %s\n", cam.UserKey)
		fmt.Printf("   Category: %s\n", cam.Category)
		fmt.Printf("   Product ID: %s\n", cam.ProductID)
		fmt.Printf("   RTSP Path: %s\n", cam.RTSPPath)
		fmt.Println()
	}

	registry, err := storageManager.GetCameraRegistry()
	if err == nil && !registry.LastUpdated.IsZero() {
		fmt.Printf("Last updated: %s\n", registry.LastUpdated.Format("2006-01-02 15:04:05"))
	}

	return nil
}

func runRefreshCameras(cmd *cobra.Command, args []string) error {
	fmt.Println("Refreshing camera discovery...")

	users, err := storageManager.ListUsers()
	if err != nil {
		return fmt.Errorf("failed to list users: %v", err)
	}

	if len(users) == 0 {
		fmt.Println("No authenticated users found.")
		fmt.Println("Use 'tuya-ipc-terminal auth add' to add users first.")
		return nil
	}

	totalCameras := 0
	successfulUsers := 0

	for _, user := range users {
		fmt.Printf("Discovering cameras for %s (%s)...\n", user.Email, user.Region)

		cameras, err := discoverCamerasForUser(&user)
		if err != nil {
			fmt.Printf("  ✗ Failed to discover cameras: %v\n", err)
			continue
		}

		if err := storageManager.UpdateCamerasForUser(user.UserKey, cameras); err != nil {
			fmt.Printf("  ✗ Failed to save cameras: %v\n", err)
			continue
		}

		fmt.Printf("  ✓ Found %d camera(s)\n", len(cameras))
		totalCameras += len(cameras)
		successfulUsers++
	}

	fmt.Printf("✓ Discovery complete!\n")
	fmt.Printf("Successfully processed %d/%d users\n", successfulUsers, len(users))
	fmt.Printf("Total cameras discovered: %d\n", totalCameras)

	return nil
}

func runCameraInfo(cmd *cobra.Command, args []string) error {
	deviceID := args[0]

	cameras, err := storageManager.GetAllCameras()
	if err != nil {
		return fmt.Errorf("failed to get cameras: %v", err)
	}

	var targetCamera *storage.CameraInfo
	for _, cam := range cameras {
		if cam.DeviceID == deviceID || cam.DeviceName == deviceID {
			targetCamera = &cam
			break
		}
	}

	if targetCamera == nil {
		return fmt.Errorf("camera not found: %s", deviceID)
	}

	fmt.Printf("Camera Information:\n")
	fmt.Printf("==================\n")
	fmt.Printf("Name: %s\n", targetCamera.DeviceName)
	fmt.Printf("Device ID: %s\n", targetCamera.DeviceID)
	fmt.Printf("UUID: %s\n", targetCamera.UUID)
	fmt.Printf("Category: %s\n", targetCamera.Category)
	fmt.Printf("Product ID: %s\n", targetCamera.ProductID)
	fmt.Printf("User: %s\n", targetCamera.UserKey)
	fmt.Printf("RTSP Path: %s\n", targetCamera.RTSPPath)

	fmt.Printf("Fetching additional information...\n")

	user, err := getUserFromKey(targetCamera.UserKey)
	if err != nil {
		fmt.Printf("Could not load user info: %v\n", err)
		return nil
	}

	httpClient := createHTTPClientWithSession(user.SessionData)
	if httpClient == nil {
		fmt.Println("Could not create HTTP client")
		return nil
	}

	webRTCConfig, err := tuya.GetWebRTCConfig(httpClient, user.SessionData.ServerHost, targetCamera.DeviceID)
	if err != nil {
		fmt.Printf("Could not get WebRTC config: %v\n", err)
		return nil
	}

	fmt.Printf("\nWebRTC Configuration:\n")
	fmt.Printf("Protocol Version: %s\n", webRTCConfig.Result.ProtocolVersion)
	fmt.Printf("Video Clarity: %d\n", webRTCConfig.Result.VideoClarity)
	fmt.Printf("Video Clarities: %v\n", webRTCConfig.Result.VedioClaritys)
	fmt.Printf("Supports WebRTC: %v\n", webRTCConfig.Result.SupportsWebrtc)
	fmt.Printf("Supports WebRTC Record: %v\n", webRTCConfig.Result.SupportWebrtcRecord)
	fmt.Printf("Supports PTZ: %v\n", webRTCConfig.Result.SupportsPtz)

	return nil
}

func discoverCamerasForUser(user *storage.UserSession) ([]storage.CameraInfo, error) {
	if user.SessionData == nil {
		return nil, errors.New("user has no valid session data")
	}

	httpClient := createHTTPClientWithSession(user.SessionData)
	if httpClient == nil {
		return nil, errors.New("failed to create HTTP client")
	}

	// Test session validity first
	_, err := tuya.GetAppInfo(httpClient, user.SessionData.ServerHost)
	if err != nil {
		return nil, fmt.Errorf("session is invalid: %v", err)
	}

	var devices []tuya.Device

	// Get home list
	homes, _ := tuya.GetHomeList(httpClient, user.SessionData.ServerHost)
	if homes != nil && len(homes.Result) > 0 {
		for _, home := range homes.Result {
			// Get room list with devices
			roomList, err := tuya.GetRoomList(httpClient, user.SessionData.ServerHost, strconv.Itoa(home.Gid))
			if err != nil {
				continue // Skip this home if we can't get rooms
			}

			// Extract cameras from rooms
			for _, room := range roomList.Result {
				for _, device := range room.DeviceList {
					// Check if device is a camera (sp = smart camera, dghsxj = another camera type)
					if (device.Category == "sp" || device.Category == "dghsxj" || device.Category == "sp_wnq") && !containsDevice(devices, device.DeviceId) {
						devices = append(devices, device)
					}
				}
			}
		}
	}

	// Get shared home list
	sharedHomes, _ := tuya.GetSharedHomeList(httpClient, user.SessionData.ServerHost)
	if sharedHomes != nil && len(sharedHomes.Result.SecurityWebCShareInfoList) > 0 {

		// Extract cameras from shared homes
		for _, sharedHome := range sharedHomes.Result.SecurityWebCShareInfoList {
			for _, device := range sharedHome.DeviceInfoList {
				// Check if device is a camera (sp = smart camera, dghsxj = another camera type)
				if (device.Category == "sp" || device.Category == "dghsxj") && !containsDevice(devices, device.DeviceId) {
					devices = append(devices, device)
				}
			}
		}
	}

	if len(devices) == 0 {
		return []storage.CameraInfo{}, nil
	}

	var allCameras []storage.CameraInfo

	for _, device := range devices {
		webrtcConfig, err := tuya.GetWebRTCConfig(httpClient, user.SessionData.ServerHost, device.DeviceId)
		if err != nil {
			continue // Skip if we can't get WebRTC config
		}

		rtspPath := storageManager.GenerateRTSPPath(device.DeviceName, device.DeviceId)

		camera := storage.CameraInfo{
			UserKey:    user.UserKey,
			DeviceID:   device.DeviceId,
			DeviceName: device.DeviceName,
			Category:   device.Category,
			RTSPPath:   rtspPath,
			ProductID:  device.ProductId,
			UUID:       device.Uuid,
			Skill:      webrtcConfig.Result.Skill,
		}

		allCameras = append(allCameras, camera)
	}

	return allCameras, nil
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

func getUserFromKey(userKey string) (*storage.UserSession, error) {
	users, err := storageManager.ListUsers()
	if err != nil {
		return nil, err
	}

	for _, user := range users {
		if user.UserKey == userKey {
			return &user, nil
		}
	}

	return nil, fmt.Errorf("user not found for key: %s", userKey)
}

func containsDevice(devices []tuya.Device, deviceID string) bool {
	for _, device := range devices {
		if device.DeviceId == deviceID {
			return true
		}
	}
	return false
}
