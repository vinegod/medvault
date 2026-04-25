package main

import (
	"log"
	"net/http"
	"os"

	"github.com/vinegod/medvault/backend/internal/version"
)

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Printf("MedVault %s starting on port %s", version.Version, port)

	// TODO: Initialize database connection
	// TODO: Run migrations
	// TODO: Set up router with chi
	// TODO: Register auth handlers
	// TODO: Register medicine handlers
	// TODO: Register sync handlers
	// TODO: Serve static PWA files from ./static

	log.Fatal(http.ListenAndServe(":"+port, nil))
}
