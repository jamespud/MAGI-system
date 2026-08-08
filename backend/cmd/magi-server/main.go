package main

import (
	"fmt"
	"os"

	"go.uber.org/fx"

	"github.com/jamespud/magi/backend/bootstrap"
)

func main() {
	configPath := "conf/magi.yaml"
	if p := os.Getenv("MAGI_CONFIG"); p != "" {
		configPath = p
	}

	cfg, err := bootstrap.LoadConfig(configPath)
	if err != nil {
		fmt.Printf("Failed to load config from %s: %v\n", configPath, err)
		os.Exit(1)
	}
	if err := cfg.Validate(); err != nil {
		fmt.Printf("Invalid config: %v\n", err)
		os.Exit(1)
	}

	app := fx.New(
		fx.Provide(func() *bootstrap.Config { return cfg }),
		bootstrap.Module,
	)

	app.Run()
}
