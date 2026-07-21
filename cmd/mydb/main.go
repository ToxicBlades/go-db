// Command mydb provides the SQL client, database server, and backup/restore
// commands.
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
	if len(os.Args) > 1 && (os.Args[1] == "backup" || os.Args[1] == "restore") {
		fileCommand(os.Args[1], os.Args[2:])
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "server" {
		serverCommand(os.Args[2:])
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "sql" {
		sqlCommand(os.Args[2:])
		return
	}
	if len(os.Args) < 2 {
		fmt.Println("usage: mydb server [options] | mydb sql [options] | mydb backup|restore <source> <destination>")
		os.Exit(1)
	}
	os.Exit(1)
}

func fileCommand(command string, args []string) {
	if len(args) != 2 {
		fatal(command, fmt.Errorf("usage: mydb %s <source-db> <destination-db>", command))
	}
	var err error
	if command == "backup" {
		err = kv.Backup(args[0], args[1])
	} else {
		err = kv.Restore(args[0], args[1])
	}
	if err != nil {
		fatal(command, err)
	}
	fmt.Printf("%s complete: %s -> %s (including WAL)\n", command, args[0], args[1])
}

func serverCommand(args []string) {
	flags := flag.NewFlagSet("server", flag.ExitOnError)
	dbPath := flags.String("db", "mydb.db", "database file")
	addr := flags.String("addr", ":5433", "TCP address to listen on")
	seedPath := flags.String("seed", "seed.sql", "SQL file to run before starting")
	username := flags.String("user", "", "TCP username (enables authentication)")
	password := flags.String("password", "", "TCP password (enables authentication)")
	flags.Parse(args)
	if (*username == "") != (*password == "") {
		fatal("server", fmt.Errorf("--user and --password must be provided together"))
	}
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
	var s *server.Server
	if *username != "" {
		s, err = server.NewWithAuth(executor, *username, *password)
	} else {
		s, err = server.New(executor)
	}
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
	username := flags.String("user", "", "TCP username")
	password := flags.String("password", "", "TCP password")
	flags.Parse(args)
	if (*username == "") != (*password == "") {
		fatal("sql", fmt.Errorf("--user and --password must be provided together"))
	}

	conn, err := net.Dial("tcp", *addr)
	if err != nil {
		fatal("connecting to server", err)
	}
	defer conn.Close()
	if *username != "" {
		if _, err := fmt.Fprintf(conn, `{"username":%q,"password":%q}`+"\n", *username, *password); err != nil {
			fatal("authenticating", err)
		}
		var response server.Response
		if err := json.NewDecoder(conn).Decode(&response); err != nil || !response.OK {
			if err == nil {
				err = fmt.Errorf("%s", response.Error)
			}
			fatal("authenticating", err)
		}
	}

	fmt.Printf("connected to mydb at %s (type SQL or exit)\n", *addr)
	scanner := bufio.NewScanner(os.Stdin)
	reader := bufio.NewReader(conn)
	var queryLines []string
	for {
		if len(queryLines) == 0 {
			fmt.Print("sql> ")
		} else {
			fmt.Print("...> ")
		}
		if !scanner.Scan() {
			break
		}
		line := strings.TrimSpace(scanner.Text())
		if len(queryLines) == 0 && line == "" {
			continue
		}
		if len(queryLines) == 0 && (strings.EqualFold(line, "exit") || strings.EqualFold(line, "quit")) {
			break
		}
		if line != "" {
			queryLines = append(queryLines, line)
		}
		query := strings.TrimSpace(strings.Join(queryLines, " "))
		if !strings.HasSuffix(query, ";") {
			continue
		}
		queryLines = nil
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
