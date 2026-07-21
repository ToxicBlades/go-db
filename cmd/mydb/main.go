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
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"mydb/kv"
	"mydb/server"
	"mydb/sql"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "server" {
		serverCommand(os.Args[2:])
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "sql" {
		sqlCommand(os.Args[2:])
		return
	}
	if len(os.Args) < 2 {
		fmt.Println("usage: mydb <path-to-db-file> | mydb server [options] | mydb sql [options]")
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

func serverCommand(args []string) {
	flags := flag.NewFlagSet("server", flag.ExitOnError)
	dbPath := flags.String("db", "mydb.db", "database file")
	addr := flags.String("addr", ":5433", "TCP address to listen on")
	seedPath := flags.String("seed", "seed.sql", "SQL file to run before starting")
	flags.Parse(args)
	store, err := kv.Open(*dbPath)
	if err != nil {
		fatal("opening store", err)
	}
	table, err := kv.NewTable(store, kv.Schema{Columns: []kv.Column{{Name: "id", Type: kv.IntType}, {Name: "name", Type: kv.StringType}, {Name: "active", Type: kv.BoolType}}})
	if err != nil {
		_ = store.Close()
		fatal("creating users table", err)
	}
	executor := sql.NewExecutor(map[string]*kv.Table{"users": table})
	if err := runSeed(executor, *seedPath); err != nil {
		_ = store.Close()
		fatal("running seed file", err)
	}
	s, err := server.New(executor)
	if err != nil {
		_ = store.Close()
		fatal("creating server", err)
	}
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	go func() { <-signals; _ = s.Close() }()
	fmt.Printf("mydb server listening on %s (database: %s)\n", *addr, *dbPath)
	if err := s.ListenAndServe(*addr); err != nil && err != net.ErrClosed {
		_ = store.Close()
		fatal("server", err)
	}
	_ = store.Close()
}

func sqlCommand(args []string) {
	flags := flag.NewFlagSet("sql", flag.ExitOnError)
	addr := flags.String("addr", ":5433", "SQL server address")
	flags.Parse(args)

	conn, err := net.Dial("tcp", *addr)
	if err != nil {
		fatal("connecting to server", err)
	}
	defer conn.Close()

	fmt.Printf("connected to mydb at %s (type SQL or exit)\n", *addr)
	scanner := bufio.NewScanner(os.Stdin)
	reader := bufio.NewReader(conn)
	for {
		fmt.Print("sql> ")
		if !scanner.Scan() {
			break
		}
		query := strings.TrimSpace(scanner.Text())
		if query == "" {
			continue
		}
		if strings.EqualFold(query, "exit") || strings.EqualFold(query, "quit") {
			break
		}
		if _, err := fmt.Fprintln(conn, query); err != nil {
			fmt.Fprintf(os.Stderr, "error sending query: %v\n", err)
			break
		}
		var response server.Response
		if err := json.NewDecoder(reader).Decode(&response); err != nil {
			fmt.Fprintf(os.Stderr, "error reading response: %v\n", err)
			break
		}
		if !response.OK {
			fmt.Printf("error: %s\n", response.Error)
			continue
		}
		if len(response.Rows) > 0 {
			fmt.Print(formatTable(response.Columns, response.Rows))
		} else {
			fmt.Printf("OK (%d rows affected)\n", response.Affected)
		}
	}
}

// formatTable renders query results as a compact, width-aware ASCII table.
// Keeping this separate from the network protocol means JSON clients still
// receive the same response while interactive users get readable output.
func formatTable(columns []string, rows []map[string]any) string {
	widths := make([]int, len(columns))
	for i, column := range columns {
		widths[i] = len(column)
	}
	values := make([][]string, len(rows))
	for i, row := range rows {
		values[i] = make([]string, len(columns))
		for j, column := range columns {
			values[i][j] = fmt.Sprint(row[column])
			if len(values[i][j]) > widths[j] {
				widths[j] = len(values[i][j])
			}
		}
	}

	var b strings.Builder
	writeRule := func() {
		b.WriteByte('+')
		for _, width := range widths {
			b.WriteString(strings.Repeat("-", width+2))
			b.WriteByte('+')
		}
		b.WriteByte('\n')
	}
	writeRow := func(values []string) {
		b.WriteByte('|')
		for i, value := range values {
			b.WriteByte(' ')
			b.WriteString(value)
			b.WriteString(strings.Repeat(" ", widths[i]-len(value)))
			b.WriteString(" |")
		}
		b.WriteByte('\n')
	}

	writeRule()
	writeRow(columns)
	writeRule()
	for _, row := range values {
		writeRow(row)
	}
	writeRule()
	return b.String()
}

func runSeed(executor *sql.Executor, path string) error {
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	var lines []string
	for _, line := range strings.Split(string(b), "\n") {
		if !strings.HasPrefix(strings.TrimSpace(line), "--") {
			lines = append(lines, line)
		}
	}
	for i, statement := range strings.Split(strings.Join(lines, "\n"), ";") {
		statement = strings.TrimSpace(statement)
		if statement == "" {
			continue
		}
		if _, err := executor.Execute(statement); err != nil {
			return fmt.Errorf("statement %d: %w", i+1, err)
		}
	}
	return nil
}

func fatal(action string, err error) {
	fmt.Fprintf(os.Stderr, "error %s: %v\n", action, err)
	os.Exit(1)
}
