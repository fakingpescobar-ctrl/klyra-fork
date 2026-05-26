package main

import (
	"fmt"
	"agentcli/pkg/tui"
)

func main() {
	sm := tui.NewSettingsModal("mock", "model", "http://localhost:8080/v1", "high", "auto", "workspace-write", "edit", 8192, 4096, 50, 50, 1000, "fast", "edit", "deep")
	fmt.Println(sm.View(80, 24))
}
