package main

import (
	"context"
	"log"
	"net/http"
	"time"
)

func main() {
	repo, err := NewRepository()
	if err != nil {
		log.Fatal(err)
	}

	server := NewServer(repo)
	startReminderLoop(repo)

	port := envOrDefault("APP_PORT", "8080")
	if port == "" {
		port = "8080"
	}

	log.Printf("waitwhat backend running on :%s", port)
	if err := http.ListenAndServe(":"+port, server.routes()); err != nil {
		log.Fatal(err)
	}
}

func startReminderLoop(repo *Repository) {
	go func() {
		ticker := time.NewTicker(1 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			if _, err := repo.DispatchDueReminders(context.Background()); err != nil {
				log.Printf("reminder dispatch skipped: %v", err)
			}
		}
	}()
}
