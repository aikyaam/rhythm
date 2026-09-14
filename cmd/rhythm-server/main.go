package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"rhythm/server/api"
)

func main() {
	port := flag.Int("port", 8765, "Server listening port")
	musicDir := flag.String("music-dir", "./music_library", "Path to root music directory")
	dbPath := flag.String("db", "./rhythm_server.db", "Path to server SQLite database")
	username := flag.String("user", "", "Server authentication username (optional)")
	password := flag.String("pass", "", "Server authentication password (optional)")
	flag.Parse()

	absMusicDir, err := filepath.Abs(*musicDir)
	if err != nil {
		log.Fatalf("Invalid music directory: %v", err)
	}
	_ = os.MkdirAll(absMusicDir, 0755)

	absDBPath, err := filepath.Abs(*dbPath)
	if err != nil {
		log.Fatalf("Invalid database path: %v", err)
	}

	server, err := api.NewServer(absMusicDir, absDBPath, *username, *password, *port)
	if err != nil {
		log.Fatalf("Failed to initialize Rhythm server: %v", err)
	}

	fmt.Printf("  • Listening Port : %d\n", *port)
	fmt.Printf("  • Music Root     : %s\n", absMusicDir)
	fmt.Printf("  • Database       : %s\n", absDBPath)
	if *password != "" {
		fmt.Printf("  • Auth Enabled   : Yes (User: %s)\n", *username)
	} else {
		fmt.Println("  • Auth Enabled   : No (Public Local Network Mode)")
	}
	fmt.Println("  • Capabilities   : [streaming, acquisition, library]")
	fmt.Printf("Server is ready. Stream URL: http://localhost:%d\n\n", *port)

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-sigChan
		fmt.Println("\nShutting down Rhythm server...")
		server.Stop()
		os.Exit(0)
	}()

	if err := server.Start(); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}
