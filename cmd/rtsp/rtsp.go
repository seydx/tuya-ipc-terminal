package rtsp

import (
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"tuya-ipc-terminal/pkg/core"
	"tuya-ipc-terminal/pkg/rtsp"
	"tuya-ipc-terminal/pkg/storage"
)

var storageManager *storage.StorageManager
var rtspServer *rtsp.RTSPServer

func SetStorageManager(sm *storage.StorageManager) {
	storageManager = sm
}

func NewRTSPCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "rtsp",
		Short: "Manage RTSP server",
		Long:  "Commands to start, stop, and manage the RTSP server for camera streaming.",
	}

	cmd.AddCommand(newStartCmd())
	cmd.AddCommand(newStopCmd())
	cmd.AddCommand(newStatusCmd())
	cmd.AddCommand(newListEndpointsCmd())

	return cmd
}

func newStartCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "start",
		Short: "Start RTSP server",
		Long:  "Start the RTSP server to provide camera streams.",
		RunE:  runStartServer,
	}

	cmd.Flags().StringP("address", "a", "127.0.0.1", "RTSP server listening address. Use \"[::]\" to start server on all available addresses.")
	cmd.Flags().IntP("port", "p", 8554, "RTSP server port")
	cmd.Flags().BoolP("daemon", "d", false, "Run as daemon (background)")

	return cmd
}

func newStopCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stop",
		Short: "Stop RTSP server",
		Long:  "Stop the running RTSP server.",
		RunE:  runStopServer,
	}
}

func newStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show RTSP server status",
		Long:  "Display current status and statistics of the RTSP server.",
		RunE:  runServerStatus,
	}
}

func newListEndpointsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list-endpoints",
		Short: "List available RTSP endpoints",
		Long:  "Display all available RTSP camera endpoints.",
		RunE:  runListEndpoints,
	}

	cmd.Flags().BoolP("online-only", "o", false, "Show only online cameras")

	return cmd
}

func runStartServer(cmd *cobra.Command, args []string) error {
	address, _ := cmd.Flags().GetString("address")
	port, _ := cmd.Flags().GetInt("port")
	daemon, _ := cmd.Flags().GetBool("daemon")

	// Check if we have any authenticated users
	users, err := storageManager.ListUsers()
	if err != nil {
		return fmt.Errorf("failed to check users: %v", err)
	}

	if len(users) == 0 {
		core.Logger.Warn().Msg("No authenticated users found.")
		core.Logger.Warn().Msg("Please add users first:")
		core.Logger.Warn().Msg("  tuya-ipc-terminal auth add [region] [email]")
		return errors.New("no authenticated users")
	}

	// Check if we have any cameras
	cameras, err := storageManager.GetAllCameras()
	if err != nil {
		return fmt.Errorf("failed to check cameras: %v", err)
	}

	if len(cameras) == 0 {
		core.Logger.Warn().Msg("No cameras found.")
		core.Logger.Warn().Msg("Please refresh camera discovery:")
		core.Logger.Warn().Msg("  tuya-ipc-terminal cameras refresh")
		return errors.New("no cameras found")
	}

	// Create and start RTSP server
	rtspServer = rtsp.NewRTSPServer(address, port, storageManager)

	core.Logger.Info().Msgf("Starting RTSP server on port %d...", port)

	if err := rtspServer.Start(); err != nil {
		return fmt.Errorf("failed to start RTSP server: %v", err)
	}

	if daemon {
		core.Logger.Info().Msgf("RTSP server started in daemon mode")
		return nil
	}

	// Wait for interrupt signal
	core.Logger.Info().Msgf("RTSP server is running. Press Ctrl+C to stop.")

	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, syscall.SIGINT, syscall.SIGTERM)

	<-signalChan

	core.Logger.Info().Msgf("Shutting down RTSP server...")
	if err := rtspServer.Stop(); err != nil {
		return fmt.Errorf("error stopping server: %v", err)
	}

	core.Logger.Info().Msgf("RTSP server stopped.")
	return nil
}

func runStopServer(cmd *cobra.Command, args []string) error {
	if rtspServer == nil {
		return errors.New("no RTSP server instance found")
	}

	if !rtspServer.IsRunning() {
		core.Logger.Info().Msg("RTSP server is not running")
		return nil
	}

	core.Logger.Info().Msg("Stopping RTSP server...")
	if err := rtspServer.Stop(); err != nil {
		return fmt.Errorf("failed to stop server: %v", err)
	}

	core.Logger.Info().Msg("RTSP server stopped.")
	return nil
}

func runServerStatus(cmd *cobra.Command, args []string) error {
	if rtspServer == nil {
		fmt.Println("RTSP Server Status: Not initialized")
		return nil
	}

	stats := rtspServer.GetStats()

	fmt.Println("RTSP Server Status:")
	fmt.Println("==================")
	fmt.Printf("Port: %d\n", stats.Port)
	fmt.Printf("Running: %v\n", stats.Running)
	fmt.Printf("Connected clients: %d\n", stats.ClientCount)
	fmt.Printf("Active streams: %d\n", stats.StreamCount)
	fmt.Printf("Total streams: %d\n", stats.TotalStreams)

	if stats.Running {
		fmt.Printf("\nServer has been running since startup\n")
		fmt.Printf("Access cameras via: rtsp://localhost:%d/[camera-path]\n", stats.Port)
	}

	return nil
}

func runListEndpoints(cmd *cobra.Command, args []string) error {
	cameras, err := storageManager.GetAllCameras()
	if err != nil {
		return fmt.Errorf("failed to get cameras: %v", err)
	}

	if len(cameras) == 0 {
		fmt.Println("No cameras found.")
		fmt.Println("Run 'tuya-ipc-terminal cameras refresh' to discover cameras.")
		return nil
	}

	if len(cameras) == 0 {
		fmt.Println("No online cameras found.")
		return nil
	}

	port := 8554
	if rtspServer != nil {
		port = rtspServer.GetPort()
	}

	fmt.Println("Available RTSP Endpoints:")
	fmt.Println("========================")

	for i, camera := range cameras {
		fmt.Printf("%d. %s\n", i+1, camera.DeviceName)
		fmt.Printf("   URL: rtsp://localhost:%d%s\n", port, camera.RTSPPath)
		fmt.Printf("   Device ID: %s\n", camera.DeviceID)
		fmt.Printf("   User: %s\n", camera.UserKey)
		fmt.Println()
	}

	registry, err := storageManager.GetCameraRegistry()
	if err == nil && !registry.LastUpdated.IsZero() {
		fmt.Printf("Camera list last updated: %s\n", registry.LastUpdated.Format("2006-01-02 15:04:05"))

		if time.Since(registry.LastUpdated) > 24*time.Hour {
			fmt.Println("⚠️  Camera list is old. Consider running 'cameras refresh'")
		}
	}

	fmt.Printf("Example usage:\n")
	fmt.Printf("  ffplay rtsp://localhost:%d%s\n", port, cameras[0].RTSPPath)
	fmt.Printf("  vlc rtsp://localhost:%d%s\n", port, cameras[0].RTSPPath)

	return nil
}
