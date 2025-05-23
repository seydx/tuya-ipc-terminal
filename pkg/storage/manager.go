package storage

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"tuya-ipc-terminal/pkg/tuya"
)

type UserSession struct {
	Region      string            `json:"region"`
	Email       string            `json:"email"`
	SessionData *tuya.SessionData `json:"sessionData"`
	LastRefresh time.Time         `json:"lastRefresh"`
	UserKey     string            `json:"userKey"`
}

type CameraInfo struct {
	UserKey    string `json:"userKey"` // region_email
	DeviceID   string `json:"deviceId"`
	DeviceName string `json:"deviceName"`
	Category   string `json:"category"`
	RTSPPath   string `json:"rtspPath"` // e.g., "/MyCamera"
	ProductID  string `json:"productId"`
	UUID       string `json:"uuid"`
	Skill      string `json:"skill"`
}

type CameraRegistry struct {
	Cameras     []CameraInfo `json:"cameras"`
	LastUpdated time.Time    `json:"lastUpdated"`
}

type StorageManager struct {
	dataDir string
}

func NewStorageManager() (*StorageManager, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}

	dataDir := filepath.Join(cwd, ".tuya-data")
	if err := os.MkdirAll(dataDir, 0700); err != nil {
		return nil, err
	}

	return &StorageManager{
		dataDir: dataDir,
	}, nil
}

func (sm *StorageManager) GetDataDir() string {
	return sm.dataDir
}

func userKey(region, email string) string {
	safeEmail := strings.ReplaceAll(strings.ReplaceAll(email, "@", "_at_"), ".", "_")
	return fmt.Sprintf("%s_%s", region, safeEmail)
}

func (sm *StorageManager) getUserFilePath(region, email string) string {
	key := userKey(region, email)
	return filepath.Join(sm.dataDir, fmt.Sprintf("user_%s.json", key))
}

func (sm *StorageManager) getCameraRegistryPath() string {
	return filepath.Join(sm.dataDir, "cameras.json")
}

func (sm *StorageManager) ListUsers() ([]UserSession, error) {
	pattern := filepath.Join(sm.dataDir, "user_*.json")
	files, err := filepath.Glob(pattern)
	if err != nil {
		return nil, err
	}

	var users []UserSession
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			continue // Skip files that can't be read
		}

		var user UserSession
		if err := json.Unmarshal(data, &user); err != nil {
			continue // Skip files that can't be parsed
		}

		users = append(users, user)
	}

	return users, nil
}

func (sm *StorageManager) GetUser(region, email string) (*UserSession, error) {
	filePath := sm.getUserFilePath(region, email)

	data, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // User not found
		}
		return nil, err
	}

	var user UserSession
	if err := json.Unmarshal(data, &user); err != nil {
		return nil, err
	}

	return &user, nil
}

func (sm *StorageManager) SaveUser(region, email string, sessionData *tuya.SessionData) error {
	user := UserSession{
		Region:      region,
		Email:       email,
		SessionData: sessionData,
		LastRefresh: time.Now(),
		UserKey:     userKey(region, email),
	}

	data, err := json.MarshalIndent(user, "", "  ")
	if err != nil {
		return err
	}

	filePath := sm.getUserFilePath(region, email)
	return os.WriteFile(filePath, data, 0600)
}

func (sm *StorageManager) RemoveUser(region, email string) error {
	filePath := sm.getUserFilePath(region, email)

	if _, err := os.Stat(filePath); err == nil {
		if err := os.Remove(filePath); err != nil {
			return err
		}
	}

	return sm.removeCamerasForUser(userKey(region, email))
}

func (sm *StorageManager) GetCameraRegistry() (*CameraRegistry, error) {
	filePath := sm.getCameraRegistryPath()

	data, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return &CameraRegistry{
				Cameras:     []CameraInfo{},
				LastUpdated: time.Time{},
			}, nil
		}
		return nil, err
	}

	var registry CameraRegistry
	if err := json.Unmarshal(data, &registry); err != nil {
		return nil, err
	}

	return &registry, nil
}

func (sm *StorageManager) SaveCameraRegistry(registry *CameraRegistry) error {
	registry.LastUpdated = time.Now()

	data, err := json.MarshalIndent(registry, "", "  ")
	if err != nil {
		return err
	}

	filePath := sm.getCameraRegistryPath()
	return os.WriteFile(filePath, data, 0600)
}

func (sm *StorageManager) UpdateCamerasForUser(userKey string, cameras []CameraInfo) error {
	registry, err := sm.GetCameraRegistry()
	if err != nil {
		return err
	}

	// Remove existing cameras for this user
	var newCameras []CameraInfo
	for _, cam := range registry.Cameras {
		if cam.UserKey != userKey {
			newCameras = append(newCameras, cam)
		}
	}

	// Add new cameras
	newCameras = append(newCameras, cameras...)
	registry.Cameras = newCameras

	return sm.SaveCameraRegistry(registry)
}

func (sm *StorageManager) removeCamerasForUser(userKey string) error {
	registry, err := sm.GetCameraRegistry()
	if err != nil {
		return err
	}

	var newCameras []CameraInfo
	for _, cam := range registry.Cameras {
		if cam.UserKey != userKey {
			newCameras = append(newCameras, cam)
		}
	}

	registry.Cameras = newCameras
	return sm.SaveCameraRegistry(registry)
}

func (sm *StorageManager) GetCamerasForUser(userKey string) ([]CameraInfo, error) {
	registry, err := sm.GetCameraRegistry()
	if err != nil {
		return nil, err
	}

	var userCameras []CameraInfo
	for _, cam := range registry.Cameras {
		if cam.UserKey == userKey {
			userCameras = append(userCameras, cam)
		}
	}

	return userCameras, nil
}

func (sm *StorageManager) GetAllCameras() ([]CameraInfo, error) {
	registry, err := sm.GetCameraRegistry()
	if err != nil {
		return nil, err
	}

	return registry.Cameras, nil
}

func (sm *StorageManager) GenerateRTSPPath(deviceName, deviceID string) string {
	// Clean device name for URL safety
	safeName := strings.ReplaceAll(deviceName, " ", "_")
	safeName = strings.ReplaceAll(safeName, "/", "_")
	safeName = strings.ReplaceAll(safeName, "\\", "_")

	// If name is empty or too generic, use device ID
	if safeName == "" || safeName == "_" {
		safeName = deviceID
	}

	return fmt.Sprintf("/%s", safeName)
}

func (sm *StorageManager) ValidateUserSession(region, email string) (bool, error) {
	user, err := sm.GetUser(region, email)
	if err != nil {
		return false, err
	}

	if user == nil {
		return false, nil
	}

	// Check if session is older than 4 days
	// It seems the cookie expires after 4 days
	if time.Since(user.LastRefresh) > 4*24*time.Hour {
		return false, nil
	}

	return true, nil
}
