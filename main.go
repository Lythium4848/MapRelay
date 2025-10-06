package main

import (
	"MapRelay/client"
	"MapRelay/logging"
	"MapRelay/server"
	"fmt"
	"os"
)

func main() {
	logging.Init()
	defer func() {
		_ = logging.Sync()
	}()

	if len(os.Args) < 2 {
		fmt.Println("Usage: program -client | -server [args...]")
		return
	}

	switch os.Args[1] {
	case "-client":
		client.RunClient(os.Args[2:])
	case "-server":
		server.RunServer(os.Args[2:])
	default:
		fmt.Println("Usage: program -client | -server [args...]")
	}
}
