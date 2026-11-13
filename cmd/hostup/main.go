package main

import (
	"fmt"
	"os"
)

var version = "dev"

func main() {
	if len(os.Args) > 1 && os.Args[1] == "version" {
		fmt.Println("hostup", version)
		return
	}

	fmt.Println("Hostup - Linux setup and management toolkit")
	fmt.Println()
	fmt.Println("Usage:")
	fmt.Println("  hostup version")
}
