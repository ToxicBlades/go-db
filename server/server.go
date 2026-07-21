// Package server exposes the SQL executor over a small, line-oriented TCP
// protocol. One request and one response are exchanged for each line.
package server

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"

	"mydb/sql"
)

type Request struct {
	Query string `json:"query"`
}
type Response struct {
	OK       bool             `json:"ok"`
	Columns  []string         `json:"columns,omitempty"`
	Rows     []map[string]any `json:"rows,omitempty"`
	Affected int              `json:"affected,omitempty"`
	Error    string           `json:"error,omitempty"`
}

type Server struct {
	Executor *sql.Executor
	mu       sync.Mutex
	ln       net.Listener
}

func New(executor *sql.Executor) (*Server, error) {
	if executor == nil {
		return nil, fmt.Errorf("nil executor")
	}
	return &Server{Executor: executor}, nil
}

// ListenAndServe listens on addr (for example ":5433") and serves clients
// until the listener is closed. It returns net.ErrClosed after Close.
func (s *Server) ListenAndServe(addr string) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	s.ln = ln
	for {
		conn, err := ln.Accept()
		if err != nil {
			if s.ln == nil {
				return net.ErrClosed
			}
			continue
		}
		go s.handle(conn)
	}
}

func (s *Server) Close() error {
	ln := s.ln
	if ln == nil {
		return nil
	}
	s.ln = nil
	return ln.Close()
}

func (s *Server) handle(conn net.Conn) {
	defer conn.Close()
	locked := false
	defer func() {
		if locked {
			s.mu.Unlock()
		}
	}()
	// Keep transaction state scoped to this client connection while sharing the
	// server's table registry and underlying tables.
	executor := sql.NewExecutor(s.Executor.Tables)
	sc := bufio.NewScanner(conn)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		query := line
		var req Request
		if strings.HasPrefix(line, "{") {
			if err := json.Unmarshal([]byte(line), &req); err != nil {
				s.write(conn, Response{Error: err.Error()})
				continue
			}
			query = req.Query
		}
		if strings.TrimSpace(query) == "" {
			s.write(conn, Response{Error: "query is required"})
			continue
		}
		if !locked {
			s.mu.Lock()
			locked = true
		}
		result, err := executor.Execute(query)
		if !executor.InTransaction() {
			s.mu.Unlock()
			locked = false
		}
		if err != nil {
			s.write(conn, Response{Error: err.Error()})
			continue
		}
		rows := make([]map[string]any, len(result.Rows))
		for i, row := range result.Rows {
			rows[i] = map[string]any(row)
		}
		s.write(conn, Response{OK: true, Columns: result.Columns, Rows: rows, Affected: result.Affected})
	}
}

func (s *Server) write(w io.Writer, response Response) {
	b, _ := json.Marshal(response)
	_, _ = fmt.Fprintf(w, "%s\n", b)
}
