package main

import (
	"fmt"
	"os"

	"rhythm/internal/cli"
	"rhythm/internal/tui"
	tea "github.com/charmbracelet/bubbletea"
)

func main() {

	if len(os.Args) > 1 {
		if handled := cli.HandleCLI(os.Args[1:]); handled {
			return
		}
	}

	svc, err := cli.InitServices()
	if err != nil {
		fmt.Printf("Fatal initialization error: %v\n", err)
		os.Exit(1)
	}
	defer svc.DB.Close()

	initialModel := tui.InitialModel(svc)
	p := tea.NewProgram(initialModel, tea.WithAltScreen())

	if _, err := p.Run(); err != nil {
		fmt.Printf("TUI execution error: %v\n", err)
		os.Exit(1)
	}
}
