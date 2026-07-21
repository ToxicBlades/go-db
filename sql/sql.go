// Package sql implements a deliberately small SQL front end for kv.Table.
package sql

import (
	"fmt"
	"os"
	"sort"
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
	Null
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
		if upper == "SELECT" || upper == "FROM" || upper == "WHERE" || upper == "INSERT" || upper == "INTO" || upper == "VALUES" || upper == "SHOW" || upper == "TABLES" || upper == "LIST" || upper == "CREATE" || upper == "TABLE" || upper == "ALTER" || upper == "ADD" || upper == "COLUMN" || upper == "DROP" || upper == "RENAME" || upper == "TO" || upper == "UPDATE" || upper == "SET" || upper == "DELETE" || upper == "AND" || upper == "OR" || upper == "EXPLAIN" {
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

type Statement interface{ isStatement() }
type Explain struct{ Statement Statement }

func (Explain) isStatement() {}

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
	case strings.EqualFold(p.cur().Lexeme, "EXPLAIN"):
		p.take()
		if p.cur().Type == EOF || p.cur().Type == Semicolon {
			err = fmt.Errorf("expected statement after EXPLAIN")
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
	w, err := p.condition()
	if err != nil {
		return nil, err
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
type Executor struct{ Tables map[string]*kv.Table }

func NewExecutor(tables map[string]*kv.Table) *Executor { return &Executor{tables} }
func (e *Executor) Execute(input string) (Result, error) {
	s, err := Parse(input)
	if err != nil {
		return Result{}, err
	}
	switch q := s.(type) {
	case Explain:
		return e.explain(q.Statement)
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
	if r[w.Column] == nil || w.Value == nil {
		return false
	}
	cmp := compare(r[w.Column], w.Value)
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
func (e *Executor) update(q Update) (Result, error) {
	t, err := e.table(q.Table)
	if err != nil {
		return Result{}, err
	}
	n, err := t.Update(func(r kv.Row) bool { return match(q.Where, r) }, q.Set)
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
		r[c] = q.Values[i]
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
func refRows(t *kv.Table) []kv.Row { r, _ := t.Scan(); return r }
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
		if !match(q.Where, r) {
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
