package main

import (
	"log"
	"os"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lmicroseconds | log.Lshortfile)
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
