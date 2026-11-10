package main

import (
	"github.com/AwayerCode/ReedHarness/apps/swe/internal/app"
	"os"
)

func main() { os.Exit(app.RunInstalled(os.Args[1:], os.Stdin, os.Stdout)) }
