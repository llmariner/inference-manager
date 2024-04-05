package main

import "log"

func main() {
	log.SetFlags(log.LstdFlags | log.Lmicroseconds | log.Lshortfile)
	_ = rootCmd.Execute()
}
