// Package sql implements a deliberately small SQL front end for kv.Table.
package sql

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"

	"mydb/kv"
)

type TokenType int

const (
	EOF TokenType = iota
	Keyword
	Identifier
	Number
	String
	Boolean
	Comma
	LParen
	RParen
	Equal
	Semicolon
)

type Token struct {
	Type   TokenType
	Lexeme string
}

func Lex(input string) ([]Token, error) {
	var out []Token
	for i := 0; i < len(input); {
		if unicode.IsSpace(rune(input[i])) {
			i++
			continue
		}
		c := input[i]
		switch c {
		case ',':
			out = append(out, Token{Comma, ","})
			i++
			continue
		case '(':
			out = append(out, Token{LParen, "("})
			i++
			continue
		case ')':
			out = append(out, Token{RParen, ")"})
			i++
			continue
		case '=':
			out = append(out, Token{Equal, "="})
			i++
			continue
		case ';':
			out = append(out, Token{Semicolon, ";"})
			i++
			continue
		case '*':
			out = append(out, Token{Identifier, "*"})
			i++
			continue
		}
		if c == '\'' || c == '"' {
			quote := c
			start := i + 1
			i++
			var b strings.Builder
			for i < len(input) && input[i] != quote {
				if input[i] == '\\' && i+1 < len(input) {
					i++
					b.WriteByte(input[i])
				} else {
					b.WriteByte(input[i])
				}
				i++
			}
			if i >= len(input) {
				return nil, fmt.Errorf("unterminated string")
			}
			i++
			out = append(out, Token{String, b.String()})
			_ = start
			continue
		}
		start := i
		for i < len(input) && (unicode.IsLetter(rune(input[i])) || unicode.IsDigit(rune(input[i])) || input[i] == '_') {
			i++
		}
		if start == i {
			return nil, fmt.Errorf("unexpected character %q", c)
		}
		word := input[start:i]
		upper := strings.ToUpper(word)
		typ := Identifier
		if upper == "SELECT" || upper == "FROM" || upper == "WHERE" || upper == "INSERT" || upper == "INTO" || upper == "VALUES" {
			typ = Keyword
		}
		if upper == "TRUE" || upper == "FALSE" {
			typ = Boolean
		}
		if _, err := strconv.Atoi(word); err == nil {
			typ = Number
		}
		out = append(out, Token{typ, word})
	}
	out = append(out, Token{Type: EOF})
	return out, nil
}

type Statement interface{ isStatement() }
type Select struct {
	Columns []string
	Table   string
	Where   *Condition
}

func (Select) isStatement() {}

type Insert struct {
	Table   string
	Columns []string
	Values  []any
}

func (Insert) isStatement() {}

type Condition struct {
	Column string
	Value  any
}

type parser struct {
	t []Token
	p int
}

func Parse(input string) (Statement, error) {
	t, e := Lex(input)
	if e != nil {
		return nil, e
	}
	p := &parser{t: t}
	if strings.EqualFold(p.cur().Lexeme, "SELECT") {
		return p.selectStmt()
	}
	if strings.EqualFold(p.cur().Lexeme, "INSERT") {
		return p.insertStmt()
	}
	return nil, fmt.Errorf("expected SELECT or INSERT")
}
func (p *parser) cur() Token { return p.t[p.p] }
func (p *parser) take() {
	if p.p < len(p.t)-1 {
		p.p++
	}
}
func (p *parser) want(s string) error {
	if !strings.EqualFold(p.cur().Lexeme, s) {
		return fmt.Errorf("expected %s, got %s", s, p.cur().Lexeme)
	}
	p.take()
	return nil
}
func (p *parser) selectStmt() (Statement, error) {
	p.take()
	var cols []string
	for {
		if p.cur().Lexeme == "*" {
			cols = append(cols, "*")
			p.take()
		} else {
			cols = append(cols, p.cur().Lexeme)
			p.take()
		}
		if p.cur().Type != Comma {
			break
		}
		p.take()
	}
	if e := p.want("FROM"); e != nil {
		return nil, e
	}
	table := p.cur().Lexeme
	p.take()
	var w *Condition
	if strings.EqualFold(p.cur().Lexeme, "WHERE") {
		p.take()
		col := p.cur().Lexeme
		p.take()
		if e := p.want("="); e != nil {
			return nil, e
		}
		v, e := value(p.cur())
		if e != nil {
			return nil, e
		}
		p.take()
		w = &Condition{col, v}
	}
	return Select{cols, table, w}, nil
}
func (p *parser) insertStmt() (Statement, error) {
	p.take()
	if e := p.want("INTO"); e != nil {
		return nil, e
	}
	table := p.cur().Lexeme
	p.take()
	if e := p.want("("); e != nil {
		return nil, e
	}
	var cols []string
	for {
		cols = append(cols, p.cur().Lexeme)
		p.take()
		if p.cur().Type != Comma {
			break
		}
		p.take()
	}
	if e := p.want(")"); e != nil {
		return nil, e
	}
	if e := p.want("VALUES"); e != nil {
		return nil, e
	}
	if e := p.want("("); e != nil {
		return nil, e
	}
	var vals []any
	for {
		v, e := value(p.cur())
		if e != nil {
			return nil, e
		}
		vals = append(vals, v)
		p.take()
		if p.cur().Type != Comma {
			break
		}
		p.take()
	}
	if e := p.want(")"); e != nil {
		return nil, e
	}
	if len(cols) != len(vals) {
		return nil, fmt.Errorf("column/value count mismatch")
	}
	return Insert{table, cols, vals}, nil
}
func value(t Token) (any, error) {
	switch t.Type {
	case Number:
		return strconv.Atoi(t.Lexeme)
	case String:
		return t.Lexeme, nil
	case Boolean:
		return strings.EqualFold(t.Lexeme, "true"), nil
	}
	return nil, fmt.Errorf("expected value")
}

type Result struct {
	Columns  []string
	Rows     []kv.Row
	Affected int
}
type Executor struct{ Tables map[string]*kv.Table }

func NewExecutor(tables map[string]*kv.Table) *Executor { return &Executor{tables} }
func (e *Executor) Execute(input string) (Result, error) {
	s, err := Parse(input)
	if err != nil {
		return Result{}, err
	}
	switch q := s.(type) {
	case Insert:
		return e.insert(q)
	case Select:
		return e.selectRows(q)
	}
	return Result{}, fmt.Errorf("unsupported statement")
}
func (e *Executor) table(name string) (*kv.Table, error) {
	t := e.Tables[strings.ToLower(name)]
	if t == nil {
		return nil, fmt.Errorf("unknown table %q", name)
	}
	return t, nil
}
func (e *Executor) insert(q Insert) (Result, error) {
	t, err := e.table(q.Table)
	if err != nil {
		return Result{}, err
	}
	r := kv.Row{}
	for i, c := range q.Columns {
		r[c] = q.Values[i]
	}
	s := t.Schema()
	if len(s.Columns) == 0 {
		return Result{}, fmt.Errorf("table has no columns")
	}
	key, ok := r[s.Columns[0].Name]
	if !ok {
		return Result{}, fmt.Errorf("insert must include key column %q", s.Columns[0].Name)
	}
	if err = t.Insert(fmt.Sprint(key), r); err != nil {
		return Result{}, err
	}
	return Result{Affected: 1}, nil
}
func (e *Executor) selectRows(q Select) (Result, error) {
	t, err := e.table(q.Table)
	if err != nil {
		return Result{}, err
	}
	rows, err := t.Scan()
	if err != nil {
		return Result{}, fmt.Errorf("reading table %q: %w (database rows may have been created with a different schema; use a new database file or restore the matching schema)", q.Table, err)
	}
	schema := t.Schema()
	var cols []string
	if len(q.Columns) == 1 && q.Columns[0] == "*" {
		for _, c := range schema.Columns {
			cols = append(cols, c.Name)
		}
	} else {
		cols = q.Columns
	}
	out := Result{Columns: cols}
	for _, r := range rows {
		if q.Where != nil && fmt.Sprint(r[q.Where.Column]) != fmt.Sprint(q.Where.Value) {
			continue
		}
		selected := kv.Row{}
		for _, c := range cols {
			if _, ok := r[c]; !ok {
				return Result{}, fmt.Errorf("unknown column %q", c)
			}
			selected[c] = r[c]
		}
		out.Rows = append(out.Rows, selected)
	}
	return out, nil
}
