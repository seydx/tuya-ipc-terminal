package cmd

import (
	"fmt"
	"os"

	"tuya-ipc-terminal/cmd/auth"
	"tuya-ipc-terminal/cmd/cameras"
	"tuya-ipc-terminal/cmd/rtsp"
	"tuya-ipc-terminal/pkg/storage"

	"github.com/spf13/cobra"
)

var (
	storageManager *storage.StorageManager
)

var rootCmd = &cobra.Command{
	Use:   "tuya-ipc-terminal",
	Short: "Tuya Smart Camera RTSP Bridge",
	Long: `A CLI tool to connect Tuya Smart Cameras to RTSP clients.

This tool allows you to:
- Authenticate with Tuya Smart accounts
- Discover cameras
- Provide RTSP endpoints for your cameras

Examples:
  tuya-ipc-terminal auth list
  tuya-ipc-terminal auth add eu-central user@example.com
  tuya-ipc-terminal cameras refresh
  tuya-ipc-terminal rtsp start --port 8554`,
}

func Execute(version string) error {
	rootCmd.Version = version
	return rootCmd.Execute()
}

func init() {
	cobra.OnInitialize(initConfig)

	// Add subcommands
	rootCmd.AddCommand(auth.NewAuthCmd())
	rootCmd.AddCommand(cameras.NewCamerasCmd())
	rootCmd.AddCommand(rtsp.NewRTSPCmd())
}

func initConfig() {
	var err error
	storageManager, err = storage.NewStorageManager()
	if err != nil {
		fmt.Println("Failed to initialize storage")
		os.Exit(1)
	}

	// Make storage manager available to subcommands
	auth.SetStorageManager(storageManager)
	cameras.SetStorageManager(storageManager)
	rtsp.SetStorageManager(storageManager)
}

func GetStorageManager() *storage.StorageManager {
	return storageManager
}
