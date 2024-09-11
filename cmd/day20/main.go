package main

import (
	"os"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Version: "indev",
	Use:     "day20",
	Short:   "Runs and displays confrontations between chess engines",
}

func main() {
	rootCmd.AddCommand(roomCmd)
	rootCmd.AddCommand(serverCmd)
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
