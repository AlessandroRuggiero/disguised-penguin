package main

import (
	"fmt"
	"os"

	"disguised-penguin/internal/cli"
	"disguised-penguin/internal/db"
)

func main() {
	store, err := db.NewStore()
	if err != nil {
		fmt.Printf("Failed to initialize database: %v\n", err)
		os.Exit(1)
	}
	defer store.Close()

	cli.SetupBindings(store)

	if err := cli.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
