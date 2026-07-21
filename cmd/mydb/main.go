// Command mydb is a tiny interactive shell for the key-value store.
// It's meant as a way to actually *see* the engine work, not a
// production interface.
//
// Usage:
//
//	go run ./cmd/mydb mydb.db
//
// Then at the prompt:
//
//	put name alice
//	get name
//	delete name
//	get name
//	exit
package main

import (
	"bufio"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"mydb/kv"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("usage: mydb <path-to-db-file>")
		os.Exit(1)
	}

	store, err := kv.Open(os.Args[1])
	if err != nil {
		fmt.Fprintf(os.Stderr, "error opening store: %v\n", err)
		os.Exit(1)
	}
	defer store.Close()

	// Ctrl-C normally terminates the process immediately, which would skip
	// Store.Close and leave buffered pages unwritten. Handle it like `exit`.
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(signals)
	go func() {
		<-signals
		if err := store.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "error closing store: %v\n", err)
			os.Exit(1)
		}
		os.Exit(0)
	}()

	fmt.Printf("mydb - connected to %s\n", os.Args[1])
	fmt.Println("commands: put <key> <value> | get <key> | delete <key> | exit")

	scanner := bufio.NewScanner(os.Stdin)
	for {
		fmt.Print("> ")
		if !scanner.Scan() {
			break
		}
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		parts := strings.SplitN(line, " ", 3)
		cmd := parts[0]

		switch cmd {
		case "put":
			if len(parts) < 3 {
				fmt.Println("usage: put <key> <value>")
				continue
			}
			if err := store.Put([]byte(parts[1]), []byte(parts[2])); err != nil {
				fmt.Printf("error: %v\n", err)
				continue
			}
			fmt.Println("OK")

		case "get":
			if len(parts) < 2 {
				fmt.Println("usage: get <key>")
				continue
			}
			val, found, err := store.Get([]byte(parts[1]))
			if err != nil {
				fmt.Printf("error: %v\n", err)
				continue
			}
			if !found {
				fmt.Println("(not found)")
				continue
			}
			fmt.Println(string(val))

		case "delete":
			if len(parts) < 2 {
				fmt.Println("usage: delete <key>")
				continue
			}
			if err := store.Delete([]byte(parts[1])); err != nil {
				fmt.Printf("error: %v\n", err)
				continue
			}
			fmt.Println("OK")

		case "exit", "quit":
			return

		default:
			fmt.Printf("unknown command: %s\n", cmd)
		}
	}
}
