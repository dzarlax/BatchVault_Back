package main

import (
	"fmt"
	"log"
	"os"

	"mobile-backend-go/database"
	demoseed "mobile-backend-go/internal/demo_seed"
)

func main() {
	database.ConnectDatabase()

	result, err := demoseed.Run(database.DB, demoseed.Options{
		Confirm: os.Getenv("DEMO_SEED_CONFIRM"),
		Mode:    demoseed.Mode(os.Getenv("DEMO_SEED_MODE")),
		Passwords: map[string]string{
			"DEMO_EN_PASSWORD": os.Getenv("DEMO_EN_PASSWORD"),
			"DEMO_SR_PASSWORD": os.Getenv("DEMO_SR_PASSWORD"),
			"DEMO_RU_PASSWORD": os.Getenv("DEMO_RU_PASSWORD"),
		},
	})
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("Seeded demo dataset %s in %s mode\n", result.DatasetVersion, result.Mode)
	for _, account := range result.Accounts {
		state := "updated"
		if account.Created {
			state = "created"
		}
		fmt.Printf("- %s %s workspace_id=%d\n", account.Username, state, account.WorkspaceID)
	}
}
