package main

import (
	"fmt"
	"os"
)

func main() {
	args := os.Args[1:]
	cmd := "app"
	if len(args) > 0 {
		cmd = args[0]
	}

	switch cmd {
	case "check":
		track := "ALL"
		if len(args) > 1 {
			track = args[1]
		}
		RunChecks(track)

	case "worker":
		name := "w1"
		crash := false
		for _, arg := range args[1:] {
			if arg == "--crash" {
				crash = true
			} else if !crash && name == "w1" {
				name = arg
			}
		}
		RunKitchenWorker(name, crash)

	case "app":
		StartApp()

	default:
		fmt.Println("Usage: go run . [app | check [A|B|C|D|E] | worker <name> [--crash]]")
		os.Exit(1)
	}
}
