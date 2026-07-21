// Package sql implements a deliberately small SQL front end for kv.Table.
package sql

import (
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
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
	Null
	Parameter
	Comma
	LParen
	RParen
	Equal
	NotEqual
	Less
	Greater
	LessEqual
	GreaterEqual
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
		if c == '?' {
			out = append(out, Token{Parameter, fmt.Sprintf("$%d", countParameters(out))})
			i++
			continue
		}
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
		case '!', '<', '>':
			typ := map[byte]TokenType{'!': NotEqual, '<': Less, '>': Greater}[c]
			lex := string(c)
			i++
			if i < len(input) && input[i] == '=' {
				if c == '!' {
					typ = NotEqual
				} else if c == '<' {
					typ = LessEqual
				} else {
					typ = GreaterEqual
				}
				lex += "="
				i++
			}
			if c == '!' && lex != "!=" {
				return nil, fmt.Errorf("unexpected character %q", c)
			}
			out = append(out, Token{typ, lex})
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
		for i < len(input) && (unicode.IsLetter(rune(input[i])) || unicode.IsDigit(rune(input[i])) || input[i] == '_' || input[i] == '.') {
			i++
		}
		if start == i {
			return nil, fmt.Errorf("unexpected character %q", c)
		}
		word := input[start:i]
		upper := strings.ToUpper(word)
		typ := Identifier
		if upper == "SELECT" || upper == "FROM" || upper == "WHERE" || upper == "INSERT" || upper == "INTO" || upper == "VALUES" || upper == "SHOW" || upper == "TABLES" || upper == "LIST" || upper == "CREATE" || upper == "TABLE" || upper == "ALTER" || upper == "ADD" || upper == "COLUMN" || upper == "DROP" || upper == "RENAME" || upper == "TO" || upper == "UPDATE" || upper == "SET" || upper == "DELETE" || upper == "AND" || upper == "OR" || upper == "EXPLAIN" || upper == "ORDER" || upper == "BY" || upper == "ASC" || upper == "DESC" || upper == "LIMIT" || upper == "OFFSET" || upper == "GROUP" || upper == "COUNT" || upper == "SUM" || upper == "AVG" || upper == "MIN" || upper == "MAX" || upper == "BEGIN" || upper == "COMMIT" || upper == "ROLLBACK" {
			typ = Keyword
		}
		if upper == "TRUE" || upper == "FALSE" {
			typ = Boolean
		}
		if upper == "NULL" {
			typ = Null
		}
		if _, err := strconv.Atoi(word); err == nil {
			typ = Number
		}
		if _, err := strconv.ParseFloat(word, 64); err == nil {
			typ = Number
		}
		out = append(out, Token{typ, word})
	}
	out = append(out, Token{Type: EOF})
	return out, nil
}

func countParameters(tokens []Token) int {
	n := 0
	for _, t := range tokens {
		if t.Type == Parameter {
			n++
		}
	}
	return n
}

type Statement interface{ isStatement() }
type Explain struct{ Statement Statement }

func (Explain) isStatement() {}

// ExplainTable describes the schema of a table rather than executing a query.
type ExplainTable struct{ Table string }

func (ExplainTable) isStatement() {}

type Select struct {
	Columns                        []string
	Table                          string
	Where                          *Condition
	OrderBy                        string
	Desc                           bool
	Limit                          int
	Offset                         int
	GroupBy                        []string
	JoinTable, JoinLeft, JoinRight string
}

func (Select) isStatement() {}

type Insert struct {
	Table   string
	Columns []string
	Values  []any
}

func (Insert) isStatement() {}

type ListTables struct{}

func (ListTables) isStatement() {}

type CreateTable struct {
	Table       string
	Columns     []kv.Column
	Constraints map[string]kv.ColumnConstraint
}

func (CreateTable) isStatement() {}

type DropTable struct{ Table string }

func (DropTable) isStatement() {}

type AlterTable struct {
	Table, Action, Name, NewName string
	Column                       kv.Column
}

func (AlterTable) isStatement() {}

type Update struct {
	Table string
	Set   map[string]any
	Where *Condition
}

func (Update) isStatement() {}

type Delete struct {
	Table string
	Where *Condition
}

func (Delete) isStatement() {}

type Begin struct{}

func (Begin) isStatement() {}

type Commit struct{}

func (Commit) isStatement() {}

type Rollback struct{}

func (Rollback) isStatement() {}

type Condition struct {
	Column      string
	Operator    string
	Value       any
	Left, Right *Condition
	Logic       string
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
	var s Statement
	var err error
	switch {
	case strings.EqualFold(p.cur().Lexeme, "BEGIN"):
		p.take()
		s = Begin{}
	case strings.EqualFold(p.cur().Lexeme, "COMMIT"):
		p.take()
		s = Commit{}
	case strings.EqualFold(p.cur().Lexeme, "ROLLBACK"):
		p.take()
		s = Rollback{}
	case strings.EqualFold(p.cur().Lexeme, "EXPLAIN"):
		p.take()
		if p.cur().Type == EOF || p.cur().Type == Semicolon {
			err = fmt.Errorf("expected statement after EXPLAIN")
			break
		}
		if strings.EqualFold(p.cur().Lexeme, "TABLE") {
			p.take()
			if p.cur().Type == EOF || p.cur().Type == Semicolon {
				err = fmt.Errorf("expected table name after EXPLAIN TABLE")
				break
			}
			s = ExplainTable{Table: p.cur().Lexeme}
			p.take()
			break
		}
		var inner Statement
		switch {
		case strings.EqualFold(p.cur().Lexeme, "SELECT"):
			inner, err = p.selectStmt()
		case strings.EqualFold(p.cur().Lexeme, "INSERT"):
			inner, err = p.insertStmt()
		case strings.EqualFold(p.cur().Lexeme, "UPDATE"):
			inner, err = p.updateStmt()
		case strings.EqualFold(p.cur().Lexeme, "DELETE"):
			inner, err = p.deleteStmt()
		default:
			err = fmt.Errorf("EXPLAIN supports SELECT, INSERT, UPDATE, or DELETE")
		}
		if err == nil {
			s = Explain{inner}
		}
	case strings.EqualFold(p.cur().Lexeme, "SELECT"):
		s, err = p.selectStmt()
	case strings.EqualFold(p.cur().Lexeme, "INSERT"):
		s, err = p.insertStmt()
	case strings.EqualFold(p.cur().Lexeme, "CREATE"):
		s, err = p.createStmt()
	case strings.EqualFold(p.cur().Lexeme, "DROP"):
		s, err = p.dropStmt()
	case strings.EqualFold(p.cur().Lexeme, "ALTER"):
		s, err = p.alterStmt()
	case strings.EqualFold(p.cur().Lexeme, "UPDATE"):
		s, err = p.updateStmt()
	case strings.EqualFold(p.cur().Lexeme, "DELETE"):
		s, err = p.deleteStmt()
	case strings.EqualFold(p.cur().Lexeme, "SHOW"):
		err = p.want("SHOW")
		if err == nil {
			err = p.want("TABLES")
		}
		if err == nil {
			s = ListTables{}
		}
	case strings.EqualFold(p.cur().Lexeme, "LIST"):
		err = p.want("LIST")
		if err == nil {
			err = p.want("TABLES")
		}
		if err == nil {
			s = ListTables{}
		}
	default:
		err = fmt.Errorf("expected SELECT, INSERT, CREATE, ALTER, DROP, UPDATE, DELETE, SHOW TABLES, or LIST TABLES")
	}
	if err != nil {
		return nil, err
	}
	// A semicolon terminates a statement. EOF is also accepted for backwards
	// compatibility with callers that submit one complete statement at a time.
	if p.cur().Type == Semicolon {
		p.take()
	}
	if p.cur().Type != EOF {
		return nil, fmt.Errorf("unexpected token after ;: %s", p.cur().Lexeme)
	}
	return s, nil
}

func (p *parser) createStmt() (Statement, error) {
	p.take()
	if e := p.want("TABLE"); e != nil {
		return nil, e
	}
	name := p.cur().Lexeme
	p.take()
	if e := p.want("("); e != nil {
		return nil, e
	}
	var cs []kv.Column
	constraints := map[string]kv.ColumnConstraint{}
	for {
		n := p.cur().Lexeme
		p.take()
		typ := p.cur().Lexeme
		p.take()
		var ct kv.ColumnType
		switch strings.ToUpper(typ) {
		case "INT":
			ct = kv.IntType
		case "STRING", "TEXT":
			ct = kv.StringType
		case "BOOL", "BOOLEAN":
			ct = kv.BoolType
		case "FLOAT", "REAL", "DOUBLE":
			ct = kv.FloatType
		case "BYTES", "BLOB":
			ct = kv.BytesType
		case "TIMESTAMP":
			ct = kv.TimestampType
		default:
			return nil, fmt.Errorf("unknown type %q", typ)
		}
		c := kv.Column{Name: n, Type: ct}
		var constraint kv.ColumnConstraint
		for strings.EqualFold(p.cur().Lexeme, "NOT") || strings.EqualFold(p.cur().Lexeme, "UNIQUE") || strings.EqualFold(p.cur().Lexeme, "REFERENCES") {
			word := strings.ToUpper(p.cur().Lexeme)
			p.take()
			switch word {
			case "NOT":
				if e := p.want("NULL"); e != nil {
					return nil, e
				}
				constraint.NotNull = true
			case "UNIQUE":
				constraint.Unique = true
			case "REFERENCES":
				rt := p.cur().Lexeme
				p.take()
				if e := p.want("("); e != nil {
					return nil, e
				}
				rc := p.cur().Lexeme
				p.take()
				if e := p.want(")"); e != nil {
					return nil, e
				}
				constraint.References = &kv.Reference{Table: rt, Column: rc}
			}
		}
		cs = append(cs, c)
		constraints[n] = constraint
		if p.cur().Type != Comma {
			break
		}
		p.take()
	}
	if e := p.want(")"); e != nil {
		return nil, e
	}
	return CreateTable{name, cs, constraints}, nil
}
func (p *parser) dropStmt() (Statement, error) {
	p.take()
	if strings.EqualFold(p.cur().Lexeme, "TABLE") {
		p.take()
		n := p.cur().Lexeme
		p.take()
		return DropTable{n}, nil
	}
	return nil, fmt.Errorf("expected TABLE")
}
func (p *parser) alterStmt() (Statement, error) {
	p.take()
	if e := p.want("TABLE"); e != nil {
		return nil, e
	}
	t := p.cur().Lexeme
	p.take()
	action := strings.ToUpper(p.cur().Lexeme)
	p.take()
	switch action {
	case "ADD":
		p.want("COLUMN")
		n := p.cur().Lexeme
		p.take()
		ty := p.cur().Lexeme
		p.take()
		var ct kv.ColumnType
		switch strings.ToUpper(ty) {
		case "INT":
			ct = kv.IntType
		case "STRING", "TEXT":
			ct = kv.StringType
		case "BOOL", "BOOLEAN":
			ct = kv.BoolType
		case "FLOAT", "REAL", "DOUBLE":
			ct = kv.FloatType
		case "BYTES", "BLOB":
			ct = kv.BytesType
		case "TIMESTAMP":
			ct = kv.TimestampType
		default:
			return nil, fmt.Errorf("unknown type %q", ty)
		}
		return AlterTable{Table: t, Action: "add", Column: kv.Column{Name: n, Type: ct}}, nil
	case "DROP":
		p.want("COLUMN")
		n := p.cur().Lexeme
		p.take()
		return AlterTable{Table: t, Action: "drop", Name: n}, nil
	case "RENAME":
		p.want("COLUMN")
		n := p.cur().Lexeme
		p.take()
		if e := p.want("TO"); e != nil {
			return nil, e
		}
		nn := p.cur().Lexeme
		p.take()
		return AlterTable{Table: t, Action: "rename", Name: n, NewName: nn}, nil
	}
	return nil, fmt.Errorf("unsupported ALTER action")
}
func (p *parser) updateStmt() (Statement, error) {
	p.take()
	t := p.cur().Lexeme
	p.take()
	if e := p.want("SET"); e != nil {
		return nil, e
	}
	set := map[string]any{}
	for {
		n := p.cur().Lexeme
		p.take()
		if e := p.want("="); e != nil {
			return nil, e
		}
		v, e := value(p.cur())
		if e != nil {
			return nil, e
		}
		p.take()
		set[n] = v
		if p.cur().Type != Comma {
			break
		}
		p.take()
	}
	w, e := p.condition()
	if e != nil {
		return nil, e
	}
	return Update{t, set, w}, nil
}
func (p *parser) deleteStmt() (Statement, error) {
	p.take()
	if e := p.want("FROM"); e != nil {
		return nil, e
	}
	t := p.cur().Lexeme
	p.take()
	w, e := p.condition()
	return Delete{t, w}, e
}
func (p *parser) condition() (*Condition, error) {
	if !strings.EqualFold(p.cur().Lexeme, "WHERE") {
		return nil, nil
	}
	p.take()
	return p.parseOr()
}
func (p *parser) parseOr() (*Condition, error) {
	left, err := p.parseAnd()
	if err != nil {
		return nil, err
	}
	for strings.EqualFold(p.cur().Lexeme, "OR") {
		p.take()
		right, e := p.parseAnd()
		if e != nil {
			return nil, e
		}
		left = &Condition{Left: left, Right: right, Logic: "OR"}
	}
	return left, nil
}
func (p *parser) parseAnd() (*Condition, error) {
	left, err := p.parsePrimary()
	if err != nil {
		return nil, err
	}
	for strings.EqualFold(p.cur().Lexeme, "AND") {
		p.take()
		right, e := p.parsePrimary()
		if e != nil {
			return nil, e
		}
		left = &Condition{Left: left, Right: right, Logic: "AND"}
	}
	return left, nil
}
func (p *parser) parsePrimary() (*Condition, error) {
	if p.cur().Type == LParen {
		p.take()
		c, err := p.parseOr()
		if err != nil {
			return nil, err
		}
		if err = p.want(")"); err != nil {
			return nil, err
		}
		return c, nil
	}
	col := p.cur().Lexeme
	p.take()
	op := p.cur().Lexeme
	if p.cur().Type != Equal && p.cur().Type != NotEqual && p.cur().Type != Less && p.cur().Type != Greater && p.cur().Type != LessEqual && p.cur().Type != GreaterEqual {
		return nil, fmt.Errorf("expected comparison operator")
	}
	p.take()
	v, err := value(p.cur())
	if err != nil {
		return nil, err
	}
	p.take()
	return &Condition{Column: col, Operator: op, Value: v}, nil
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
		if isAggregateName(p.cur().Lexeme) && p.t[p.p+1].Type == LParen {
			fn := strings.ToUpper(p.cur().Lexeme)
			p.take()
			p.take()
			arg := p.cur().Lexeme
			p.take()
			if e := p.want(")"); e != nil {
				return nil, e
			}
			cols = append(cols, fn+"("+arg+")")
		} else if p.cur().Lexeme == "*" {
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
	var joinTable, joinLeft, joinRight string
	if strings.EqualFold(p.cur().Lexeme, "JOIN") || (strings.EqualFold(p.cur().Lexeme, "INNER") && strings.EqualFold(p.t[p.p+1].Lexeme, "JOIN")) {
		if strings.EqualFold(p.cur().Lexeme, "INNER") {
			p.take()
		}
		p.take()
		joinTable = p.cur().Lexeme
		p.take()
		if e := p.want("ON"); e != nil {
			return nil, e
		}
		joinLeft = p.cur().Lexeme
		p.take()
		if e := p.want("="); e != nil {
			return nil, e
		}
		joinRight = p.cur().Lexeme
		p.take()
	}
	w, err := p.condition()
	if err != nil {
		return nil, err
	}
	q := Select{Columns: cols, Table: table, Where: w, Limit: -1, JoinTable: joinTable, JoinLeft: joinLeft, JoinRight: joinRight}
	if strings.EqualFold(p.cur().Lexeme, "GROUP") {
		p.take()
		if e := p.want("BY"); e != nil {
			return nil, e
		}
		for {
			q.GroupBy = append(q.GroupBy, p.cur().Lexeme)
			p.take()
			if p.cur().Type != Comma {
				break
			}
			p.take()
		}
	}
	if strings.EqualFold(p.cur().Lexeme, "ORDER") {
		p.take()
		if e := p.want("BY"); e != nil {
			return nil, e
		}
		q.OrderBy = p.cur().Lexeme
		p.take()
		if strings.EqualFold(p.cur().Lexeme, "ASC") || strings.EqualFold(p.cur().Lexeme, "DESC") {
			q.Desc = strings.EqualFold(p.cur().Lexeme, "DESC")
			p.take()
		}
	}
	if strings.EqualFold(p.cur().Lexeme, "LIMIT") {
		p.take()
		n, e := value(p.cur())
		if e != nil {
			return nil, e
		}
		q.Limit = n.(int)
		p.take()
	}
	if strings.EqualFold(p.cur().Lexeme, "OFFSET") {
		p.take()
		n, e := value(p.cur())
		if e != nil {
			return nil, e
		}
		q.Offset = n.(int)
		p.take()
	}
	return q, nil
}
func isAggregateName(s string) bool {
	u := strings.ToUpper(s)
	return u == "COUNT" || u == "SUM" || u == "AVG" || u == "MIN" || u == "MAX"
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
	case Parameter:
		i, err := strconv.Atoi(strings.TrimPrefix(t.Lexeme, "$"))
		if err != nil {
			return nil, fmt.Errorf("invalid parameter")
		}
		return parameterValue{index: i}, nil
	case Number:
		if strings.Contains(t.Lexeme, ".") {
			return strconv.ParseFloat(t.Lexeme, 64)
		}
		return strconv.Atoi(t.Lexeme)
	case String:
		return t.Lexeme, nil
	case Boolean:
		return strings.EqualFold(t.Lexeme, "true"), nil
	case Null:
		return nil, nil
	}
	return nil, fmt.Errorf("expected value")
}

type Result struct {
	Columns  []string
	Rows     []kv.Row
	Affected int
}
type transactionSnapshot struct {
	t    *kv.Table
	rows []kv.Row
}

type Executor struct {
	Tables map[string]*kv.Table
	tx     []transactionSnapshot
}

// PreparedStatement is a parsed SQL statement that can be executed repeatedly
// with different values. Parameters are written as '?' in the SQL text.
type PreparedStatement struct {
	executor   *Executor
	statement  Statement
	parameters int
}

// Prepare parses input once and returns a reusable parameterized statement.
func (e *Executor) Prepare(input string) (*PreparedStatement, error) {
	s, err := Parse(input)
	if err != nil {
		return nil, err
	}
	return &PreparedStatement{executor: e, statement: s, parameters: countStatementParameters(s)}, nil
}

// Execute binds args to '?' parameters and executes the prepared statement.
func (p *PreparedStatement) Execute(args ...any) (Result, error) {
	if len(args) != p.parameters {
		return Result{}, fmt.Errorf("expected %d parameters, got %d", p.parameters, len(args))
	}
	s, err := bindStatement(p.statement, args)
	if err != nil {
		return Result{}, err
	}
	return p.executor.executeStatement(s)
}

type parameterValue struct{ index int }

func countStatementParameters(s Statement) int {
	max := -1
	var walk func(any)
	walk = func(v any) {
		switch x := v.(type) {
		case parameterValue:
			if x.index > max {
				max = x.index
			}
		case Insert:
			for _, v := range x.Values {
				walk(v)
			}
		case Update:
			for _, v := range x.Set {
				walk(v)
			}
			walk(x.Where)
		case Delete:
			walk(x.Where)
		case Select:
			walk(x.Where)
		case Explain:
			walk(x.Statement)
		case *Condition:
			if x != nil {
				walk(x.Value)
				walk(x.Left)
				walk(x.Right)
			}
		}
	}
	walk(s)
	return max + 1
}
func bindStatement(s Statement, args []any) (Statement, error) {
	bind := func(v any) any {
		if p, ok := v.(parameterValue); ok {
			return args[p.index]
		}
		return v
	}
	var cond func(*Condition) *Condition
	cond = func(c *Condition) *Condition {
		if c == nil {
			return nil
		}
		return &Condition{Column: c.Column, Operator: c.Operator, Value: bind(c.Value), Left: cond(c.Left), Right: cond(c.Right), Logic: c.Logic}
	}
	switch x := s.(type) {
	case Insert:
		values := append([]any(nil), x.Values...)
		for i := range values {
			values[i] = bind(values[i])
		}
		x.Values = values
		return x, nil
	case Update:
		set := make(map[string]any, len(x.Set))
		for k, v := range x.Set {
			set[k] = bind(v)
		}
		x.Set = set
		x.Where = cond(x.Where)
		return x, nil
	case Delete:
		x.Where = cond(x.Where)
		return x, nil
	case Select:
		x.Where = cond(x.Where)
		return x, nil
	case Explain:
		inner, err := bindStatement(x.Statement, args)
		return Explain{inner}, err
	default:
		return s, nil
	}
}

func NewExecutor(tables map[string]*kv.Table) *Executor { return &Executor{Tables: tables} }
func (e *Executor) Execute(input string) (Result, error) {
	parts := splitStatements(input)
	if len(parts) == 0 {
		return Result{}, fmt.Errorf("empty request")
	}
	containsTxControl := false
	for _, part := range parts {
		s, err := Parse(part)
		if err != nil {
			return Result{}, err
		}
		switch s.(type) {
		case Begin, Commit, Rollback:
			containsTxControl = true
		}
	}
	if len(parts) == 1 {
		return e.executeOne(parts[0])
	}
	if e.inTransaction() || containsTxControl {
		var out Result
		for _, part := range parts {
			var err error
			out, err = e.executeOne(part)
			if err != nil {
				return Result{}, err
			}
		}
		return out, nil
	}
	type snap struct {
		t    *kv.Table
		rows []kv.Row
	}
	var snaps []snap
	for _, t := range e.Tables {
		rows, err := t.Scan()
		if err != nil {
			return Result{}, err
		}
		snaps = append(snaps, snap{t, rows})
	}
	var out Result
	for _, part := range parts {
		r, err := e.executeOne(part)
		if err != nil {
			for _, s := range snaps {
				_, _ = s.t.DeleteWhere(func(kv.Row) bool { return true })
				for _, row := range s.rows {
					_ = s.t.Insert(fmt.Sprint(row[s.t.Schema().Columns[0].Name]), row)
				}
			}
			return Result{}, fmt.Errorf("transaction rolled back: %w", err)
		}
		out = r
	}
	return out, nil
}
func (e *Executor) inTransaction() bool { return e.tx != nil }

// InTransaction reports whether this executor owns an open explicit
// transaction. The server uses it to keep that transaction isolated.
func (e *Executor) InTransaction() bool { return e.inTransaction() }

func (e *Executor) begin() (Result, error) {
	if e.inTransaction() {
		return Result{}, fmt.Errorf("transaction already in progress")
	}
	e.tx = make([]transactionSnapshot, 0, len(e.Tables))
	for _, t := range e.Tables {
		rows, err := t.Scan()
		if err != nil {
			e.tx = nil
			return Result{}, err
		}
		e.tx = append(e.tx, transactionSnapshot{t: t, rows: rows})
	}
	return Result{}, nil
}

func (e *Executor) commit() (Result, error) {
	if !e.inTransaction() {
		return Result{}, fmt.Errorf("no transaction in progress")
	}
	e.tx = nil
	return Result{}, nil
}

func (e *Executor) rollback() (Result, error) {
	if !e.inTransaction() {
		return Result{}, fmt.Errorf("no transaction in progress")
	}
	for _, snap := range e.tx {
		if _, err := snap.t.DeleteWhere(func(kv.Row) bool { return true }); err != nil {
			return Result{}, err
		}
		for _, row := range snap.rows {
			if err := snap.t.Insert(fmt.Sprint(row[snap.t.Schema().Columns[0].Name]), row); err != nil {
				return Result{}, err
			}
		}
	}
	e.tx = nil
	return Result{}, nil
}
func splitStatements(s string) []string {
	var out []string
	start := 0
	quote := byte(0)
	for i := 0; i < len(s); i++ {
		if quote != 0 {
			if s[i] == quote {
				quote = 0
			}
			continue
		}
		if s[i] == '\'' || s[i] == '"' {
			quote = s[i]
		} else if s[i] == ';' {
			if x := strings.TrimSpace(s[start:i]); x != "" {
				out = append(out, x)
			}
			start = i + 1
		}
	}
	if x := strings.TrimSpace(s[start:]); x != "" {
		out = append(out, x)
	}
	return out
}
func (e *Executor) executeOne(input string) (Result, error) {
	s, err := Parse(input)
	if err != nil {
		return Result{}, err
	}
	return e.executeStatement(s)
}
func (e *Executor) executeStatement(s Statement) (Result, error) {
	switch q := s.(type) {
	case Begin:
		return e.begin()
	case Commit:
		return e.commit()
	case Rollback:
		return e.rollback()
	case Explain:
		return e.explain(q.Statement)
	case ExplainTable:
		return e.explainTable(q)
	case Insert:
		return e.insert(q)
	case Select:
		return e.selectRows(q)
	case ListTables:
		return e.listTables()
	case Update:
		return e.update(q)
	case Delete:
		return e.delete(q)
	case DropTable:
		return e.drop(q)
	case AlterTable:
		return e.alter(q)
	case CreateTable:
		return e.create(q)
	}
	return Result{}, fmt.Errorf("unsupported statement")
}

func (e *Executor) explain(s Statement) (Result, error) {
	var plan string
	switch q := s.(type) {
	case Select:
		filter := "none"
		if q.Where != nil {
			filter = "apply WHERE predicate"
		}
		plan = fmt.Sprintf("Seq Scan on %s; filter: %s; projection: %s", q.Table, filter, strings.Join(q.Columns, ", "))
	case Insert:
		plan = fmt.Sprintf("Insert into %s; columns: %s", q.Table, strings.Join(q.Columns, ", "))
	case Update:
		plan = fmt.Sprintf("Seq Scan on %s; update columns: %s", q.Table, mapKeys(q.Set))
	case Delete:
		plan = fmt.Sprintf("Seq Scan on %s; delete matching rows", q.Table)
	default:
		return Result{}, fmt.Errorf("unsupported statement for EXPLAIN")
	}
	return Result{Columns: []string{"plan"}, Rows: []kv.Row{{"plan": plan}}}, nil
}

func (e *Executor) explainTable(q ExplainTable) (Result, error) {
	t, err := e.table(q.Table)
	if err != nil {
		return Result{}, err
	}
	columns := []string{"column", "type", "nullable", "unique"}
	result := Result{Columns: columns}
	schema := t.Schema()
	for _, column := range schema.Columns {
		constraint := schema.Constraints[column.Name]
		result.Rows = append(result.Rows, kv.Row{
			"column":   column.Name,
			"type":     columnTypeName(column.Type),
			"nullable": !constraint.NotNull,
			"unique":   constraint.Unique,
		})
	}
	return result, nil
}

func columnTypeName(t kv.ColumnType) string {
	switch t {
	case kv.IntType:
		return "INT"
	case kv.StringType:
		return "STRING"
	case kv.BoolType:
		return "BOOL"
	case kv.FloatType:
		return "FLOAT"
	case kv.BytesType:
		return "BYTES"
	case kv.TimestampType:
		return "TIMESTAMP"
	default:
		return "UNKNOWN"
	}
}

func mapKeys(m map[string]any) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return strings.Join(keys, ", ")
}
func match(w *Condition, r kv.Row) bool {
	if w == nil {
		return true
	}
	if w.Logic == "AND" {
		return match(w.Left, r) && match(w.Right, r)
	}
	if w.Logic == "OR" {
		return match(w.Left, r) || match(w.Right, r)
	}
	if lookup(r, w.Column) == nil || w.Value == nil {
		return false
	}
	cmp := compare(lookup(r, w.Column), w.Value)
	switch w.Operator {
	case "=":
		return cmp == 0
	case "!=":
		return cmp != 0
	case "<":
		return cmp < 0
	case ">":
		return cmp > 0
	case "<=":
		return cmp <= 0
	case ">=":
		return cmp >= 0
	}
	return false
}
func compare(a, b any) int {
	if af, ok := number(a); ok {
		if bf, ok := number(b); ok {
			if af < bf {
				return -1
			}
			if af > bf {
				return 1
			}
			return 0
		}
	}
	if x, ok := a.(int); ok {
		if y, ok := b.(int); ok {
			if x < y {
				return -1
			}
			if x > y {
				return 1
			}
			return 0
		}
	}
	if x, ok := a.(string); ok {
		if y, ok := b.(string); ok {
			if x < y {
				return -1
			}
			if x > y {
				return 1
			}
			return 0
		}
	}
	if x, ok := a.(bool); ok {
		if y, ok := b.(bool); ok {
			if x == y {
				return 0
			}
			if !x {
				return -1
			}
			return 1
		}
	}
	left, right := fmt.Sprint(a), fmt.Sprint(b)
	if left < right {
		return -1
	}
	if left > right {
		return 1
	}
	return 0
}
func number(v any) (float64, bool) {
	switch x := v.(type) {
	case int:
		return float64(x), true
	case float64:
		return x, true
	}
	return 0, false
}
func (e *Executor) update(q Update) (Result, error) {
	t, err := e.table(q.Table)
	if err != nil {
		return Result{}, err
	}
	set := kv.Row{}
	for name, v := range q.Set {
		found := false
		for _, c := range t.Schema().Columns {
			if strings.EqualFold(c.Name, name) {
				cv, ce := coerceValue(v, c.Type)
				if ce != nil {
					return Result{}, ce
				}
				set[name] = cv
				found = true
				break
			}
		}
		if !found {
			return Result{}, fmt.Errorf("unknown column %q", name)
		}
	}
	n, err := t.Update(func(r kv.Row) bool { return match(q.Where, r) }, set)
	return Result{Affected: n}, err
}
func (e *Executor) delete(q Delete) (Result, error) {
	t, err := e.table(q.Table)
	if err != nil {
		return Result{}, err
	}
	n, err := t.DeleteWhere(func(r kv.Row) bool { return match(q.Where, r) })
	return Result{Affected: n}, err
}
func (e *Executor) drop(q DropTable) (Result, error) {
	n := strings.ToLower(q.Table)
	t, ok := e.Tables[n]
	if !ok {
		return Result{}, fmt.Errorf("unknown table %q", q.Table)
	}
	delete(e.Tables, n)
	return Result{Affected: 1}, t.Close()
}
func (e *Executor) alter(q AlterTable) (Result, error) {
	t, err := e.table(q.Table)
	if err != nil {
		return Result{}, err
	}
	s := t.Schema()
	switch q.Action {
	case "add":
		s.Columns = append(s.Columns, q.Column)
	case "drop":
		var c []kv.Column
		for _, x := range s.Columns {
			if !strings.EqualFold(x.Name, q.Name) {
				c = append(c, x)
			}
		}
		s.Columns = c
	case "rename":
		for i := range s.Columns {
			if strings.EqualFold(s.Columns[i].Name, q.Name) {
				s.Columns[i].Name = q.NewName
			}
		}
	}
	if err = t.Alter(s); err != nil {
		return Result{}, err
	}
	return Result{Affected: 1}, nil
}
func (e *Executor) create(q CreateTable) (Result, error) {
	n := strings.ToLower(q.Table)
	if e.Tables[n] != nil {
		return Result{}, fmt.Errorf("table %q already exists", q.Table)
	}
	f, err := os.CreateTemp("", "mydb-table-")
	if err != nil {
		return Result{}, err
	}
	path := f.Name()
	f.Close()
	s, err := kv.Open(path)
	if err != nil {
		return Result{}, err
	}
	// Every SQL table has a stable first column for the key/value store.  Make
	// it implicit when the user did not declare one, so INSERTs can omit it.
	columns := q.Columns
	hasID := false
	for _, c := range columns {
		if strings.EqualFold(c.Name, "id") {
			hasID = true
			break
		}
	}
	if !hasID {
		columns = append([]kv.Column{{Name: "id", Type: kv.IntType}}, columns...)
	}
	t, err := kv.NewTable(s, kv.Schema{Columns: columns, Constraints: q.Constraints})
	if err != nil {
		s.Close()
		return Result{}, err
	}
	e.Tables[n] = t
	return Result{Affected: 1}, nil
}

func (e *Executor) listTables() (Result, error) {
	result := Result{Columns: []string{"table_name"}}
	for name := range e.Tables {
		result.Rows = append(result.Rows, kv.Row{"table_name": name})
	}
	sort.Slice(result.Rows, func(i, j int) bool {
		return result.Rows[i]["table_name"].(string) < result.Rows[j]["table_name"].(string)
	})
	return result, nil
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
		v := q.Values[i]
		for _, sc := range t.Schema().Columns {
			if strings.EqualFold(sc.Name, c) {
				var ce error
				v, ce = coerceValue(v, sc.Type)
				if ce != nil {
					return Result{}, ce
				}
				break
			}
		}
		r[c] = v
	}
	s := t.Schema()
	if len(s.Columns) == 0 {
		return Result{}, fmt.Errorf("table has no columns")
	}
	key, ok := r[s.Columns[0].Name]
	if !ok {
		// SQL-created tables use an integer id as their first column. Deriving
		// the next value from live rows also makes this work after reopening a
		// database without maintaining a separate counter.
		if s.Columns[0].Type != kv.IntType {
			return Result{}, fmt.Errorf("insert must include key column %q", s.Columns[0].Name)
		}
		rows, scanErr := t.Scan()
		if scanErr != nil {
			return Result{}, scanErr
		}
		next := 1
		for _, row := range rows {
			if id, ok := row[s.Columns[0].Name].(int); ok && id >= next {
				next = id + 1
			}
		}
		key = next
		r[s.Columns[0].Name] = next
	}
	for _, c := range s.Columns {
		cc := s.Constraints[c.Name]
		if cc.References != nil && r[c.Name] != nil {
			ref, e := e.table(cc.References.Table)
			if e != nil {
				return Result{}, e
			}
			found := false
			for _, rr := range refRows(ref) {
				if rr[cc.References.Column] == r[c.Name] {
					found = true
					break
				}
			}
			if !found {
				return Result{}, fmt.Errorf("foreign key constraint failed: %s", c.Name)
			}
		}
	}
	if err = t.Insert(fmt.Sprint(key), r); err != nil {
		return Result{}, err
	}
	return Result{Affected: 1}, nil
}
func coerceValue(v any, typ kv.ColumnType) (any, error) {
	if v == nil {
		return nil, nil
	}
	switch typ {
	case kv.FloatType:
		if i, ok := v.(int); ok {
			return float64(i), nil
		}
		if _, ok := v.(float64); ok {
			return v, nil
		}
	case kv.BytesType:
		if s, ok := v.(string); ok {
			return []byte(s), nil
		}
		if b, ok := v.([]byte); ok {
			return b, nil
		}
	case kv.TimestampType:
		if s, ok := v.(string); ok {
			t, e := time.Parse(time.RFC3339, s)
			if e != nil {
				return nil, fmt.Errorf("invalid timestamp %q", s)
			}
			return t.UTC(), nil
		}
		if t, ok := v.(time.Time); ok {
			return t, nil
		}
	default:
		return v, nil
	}
	return nil, fmt.Errorf("value has wrong type for %s", columnTypeName(typ))
}
func refRows(t *kv.Table) []kv.Row { r, _ := t.Scan(); return r }
func (e *Executor) selectRows(q Select) (Result, error) {
	t, err := e.table(q.Table)
	if err != nil {
		return Result{}, err
	}
	rows, err := t.Scan()
	if q.Where != nil && q.JoinTable == "" && q.Where.Operator == "=" && q.Where.Column != "" && q.Where.Left == nil && q.Where.Right == nil {
		if indexed, findErr := t.Find(q.Where.Column, q.Where.Value); findErr != nil {
			return Result{}, findErr
		} else if indexed != nil {
			rows = indexed
		}
	}
	if err != nil {
		return Result{}, fmt.Errorf("reading table %q: %w (database rows may have been created with a different schema; use a new database file or restore the matching schema)", q.Table, err)
	}
	if q.JoinTable != "" {
		jt, e := e.table(q.JoinTable)
		if e != nil {
			return Result{}, e
		}
		jr, e := jt.Scan()
		if e != nil {
			return Result{}, e
		}
		var joined []kv.Row
		for _, a := range rows {
			for _, b := range jr {
				if compare(lookup(a, q.JoinLeft), lookup(b, q.JoinRight)) == 0 {
					n := kv.Row{}
					for k, v := range a {
						n[k] = v
					}
					for k, v := range b {
						if _, ok := n[k]; ok {
							n[q.JoinTable+"."+k] = v
						} else {
							n[k] = v
						}
					}
					joined = append(joined, n)
				}
			}
		}
		rows = joined
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
	filtered := rows[:0]
	for _, r := range rows {
		if match(q.Where, r) {
			filtered = append(filtered, r)
		}
	}
	for _, c := range q.GroupBy {
		for _, r := range filtered {
			if lookup(r, c) == nil {
				return Result{}, fmt.Errorf("unknown column %q", c)
			}
			break
		}
	}
	aggregate := false
	for _, c := range cols {
		if isAggregate(c) {
			aggregate = true
		}
	}
	if aggregate || len(q.GroupBy) > 0 {
		groups := map[string][]kv.Row{}
		order := []string{}
		for _, r := range filtered {
			key := groupKey(q.GroupBy, r)
			if _, ok := groups[key]; !ok {
				order = append(order, key)
			}
			groups[key] = append(groups[key], r)
		}
		if len(order) == 0 && aggregate && len(q.GroupBy) == 0 {
			order = append(order, "")
		}
		out := Result{Columns: cols}
		for _, key := range order {
			row, err := aggregateRow(cols, q.GroupBy, groups[key])
			if err != nil {
				return Result{}, err
			}
			out.Rows = append(out.Rows, row)
		}
		return applyPagingAndOrder(out, q)
	}
	out := Result{Columns: cols}
	for _, r := range filtered {
		selected := kv.Row{}
		for _, c := range cols {
			if lookup(r, c) == nil {
				return Result{}, fmt.Errorf("unknown column %q", c)
			}
			selected[c] = lookup(r, c)
		}
		out.Rows = append(out.Rows, selected)
	}
	return applyPagingAndOrder(out, q)
}
func lookup(r kv.Row, name string) any {
	if v, ok := r[name]; ok {
		return v
	}
	if i := strings.Index(name, "."); i >= 0 {
		return r[name[i+1:]]
	}
	return nil
}

func isAggregate(s string) bool {
	u := strings.ToUpper(s)
	return strings.HasPrefix(u, "COUNT(") || strings.HasPrefix(u, "SUM(") || strings.HasPrefix(u, "AVG(") || strings.HasPrefix(u, "MIN(") || strings.HasPrefix(u, "MAX(")
}
func groupKey(cols []string, r kv.Row) string {
	var b strings.Builder
	for _, c := range cols {
		b.WriteString(fmt.Sprintf("%#v|", r[c]))
	}
	return b.String()
}
func aggregateRow(cols, groups []string, rows []kv.Row) (kv.Row, error) {
	out := kv.Row{}
	for _, c := range groups {
		if len(rows) > 0 {
			out[c] = rows[0][c]
		}
	}
	for _, expr := range cols {
		u := strings.ToUpper(expr)
		open := strings.Index(expr, "(")
		if open < 0 {
			if len(rows) == 0 {
				return nil, fmt.Errorf("group column %q has no rows", expr)
			}
			out[expr] = rows[0][expr]
			continue
		}
		col := strings.TrimSpace(expr[open+1 : len(expr)-1])
		if col == "*" {
			if strings.HasPrefix(u, "COUNT") {
				out[expr] = len(rows)
			}
			continue
		}
		var vals []any
		for _, r := range rows {
			if v := r[col]; v != nil {
				vals = append(vals, v)
			}
		}
		if strings.HasPrefix(u, "COUNT") {
			out[expr] = len(vals)
		} else if len(vals) == 0 {
			out[expr] = nil
		} else if strings.HasPrefix(u, "MIN") || strings.HasPrefix(u, "MAX") {
			best := vals[0]
			for _, v := range vals[1:] {
				cmp := compare(v, best)
				if (strings.HasPrefix(u, "MIN") && cmp < 0) || (strings.HasPrefix(u, "MAX") && cmp > 0) {
					best = v
				}
			}
			out[expr] = best
		} else {
			sum := 0.0
			for _, v := range vals {
				n, ok := number(v)
				if !ok {
					return nil, fmt.Errorf("aggregate %s requires numeric column", expr)
				}
				sum += n
			}
			if strings.HasPrefix(u, "AVG") {
				sum /= float64(len(vals))
			}
			if strings.HasPrefix(u, "SUM") && allInts(vals) {
				out[expr] = int(sum)
			} else {
				out[expr] = sum
			}
		}
	}
	return out, nil
}
func allInts(v []any) bool {
	for _, x := range v {
		if _, ok := x.(int); !ok {
			return false
		}
	}
	return true
}
func applyPagingAndOrder(out Result, q Select) (Result, error) {
	if q.OrderBy != "" {
		sort.SliceStable(out.Rows, func(i, j int) bool {
			c := compare(out.Rows[i][q.OrderBy], out.Rows[j][q.OrderBy])
			if q.Desc {
				return c > 0
			}
			return c < 0
		})
	}
	start := q.Offset
	if start < 0 {
		start = 0
	}
	if start > len(out.Rows) {
		start = len(out.Rows)
	}
	end := len(out.Rows)
	if q.Limit >= 0 && start+q.Limit < end {
		end = start + q.Limit
	}
	out.Rows = out.Rows[start:end]
	return out, nil
}
