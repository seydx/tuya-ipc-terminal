package main

import (
	"fmt"
	"os"

	"tuya-ipc-terminal/cmd"
	"tuya-ipc-terminal/pkg/core"
)

const VERSION = "0.0.2"

func main() {
	core.InitLogger()

	if err := cmd.Execute(VERSION); err != nil {
		fmt.Println("Command execution failed")
		os.Exit(1)
	}
}
