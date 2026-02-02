package main

import (
	"client/internal"
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	model := internal.NewChatModel()
	_, err := tea.NewProgram(model).Run()
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "Could not start ChatModel, error: %v", err)
		os.Exit(1)
	}
}
