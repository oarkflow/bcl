package main

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// ------------------------------------------------------
// Token definitions
// ------------------------------------------------------

type TokenType string

const (
	// Special tokens
	ILLEGAL TokenType = "ILLEGAL"
	EOF               = "EOF"

	// Identifiers + literals
	IDENT  = "IDENT"
	NUMBER = "NUMBER"
	STRING = "STRING"

	// Delimiters and punctuation
	COMMA     = ","
	SEMICOLON = ";"
	LPAREN    = "("
	RPAREN    = ")"
	LBRACE    = "{"
	RBRACE    = "}"

	// ADD: New token definitions for additional statements.
	SELECT TokenType = "SELECT"
	UPDATE TokenType = "UPDATE"
	DELETE TokenType = "DELETE"
)

// Keywords for the DSL. They are lower-cased for case-insensitive matching.
const (
	UPKeyword   = "up"
	DOWNKeyword = "down"
	CREATE      = "create"
	TABLE       = "table"
	INSERT      = "insert"
	INTO        = "into"
	VALUES      = "values"
	ALTER       = "alter"
	RENAME      = "rename"
	ADD         = "add"
	DROP        = "drop"
	CHANGE      = "change"
)

var keywords = map[string]TokenType{
	UPKeyword:   TokenType("UP"),
	DOWNKeyword: TokenType("DOWN"),
	CREATE:      TokenType("CREATE"),
	TABLE:       TokenType("TABLE"),
	INSERT:      TokenType("INSERT"),
	INTO:        TokenType("INTO"),
	VALUES:      TokenType("VALUES"),
	ALTER:       TokenType("ALTER"),
	RENAME:      TokenType("RENAME"),
	ADD:         TokenType("ADD"),
	DROP:        TokenType("DROP"),
	CHANGE:      TokenType("CHANGE"),
	// New entries with string keys:
	"select": SELECT,
	"update": UPDATE,
	"delete": DELETE,
}

// Token represents a lexical token.
type Token struct {
	Type    TokenType
	Literal string
}

func LookupIdent(ident string) TokenType {
	if tok, ok := keywords[strings.ToLower(ident)]; ok {
		return tok
	}
	return IDENT
}

// ------------------------------------------------------
// Lexer
// ------------------------------------------------------

type Lexer struct {
	input        string
	position     int  // current position in input (points to current char)
	readPosition int  // next reading position in input
	ch           rune // current char under examination
}

func NewLexer(input string) *Lexer {
	l := &Lexer{input: input}
	l.readChar() // initialize first char
	return l
}

func (l *Lexer) readChar() {
	if l.readPosition >= len(l.input) {
		l.ch = 0 // 0 means EOF
		return
	}
	r, size := utf8.DecodeRuneInString(l.input[l.readPosition:])
	l.ch = r
	l.position = l.readPosition
	l.readPosition += size
}

func (l *Lexer) peekChar() rune {
	if l.readPosition >= len(l.input) {
		return 0
	}
	r, _ := utf8.DecodeRuneInString(l.input[l.readPosition:])
	return r
}

func (l *Lexer) NextToken() Token {
	var tok Token

	l.skipWhitespace()

	switch l.ch {
	case '{':
		tok = newToken(LBRACE, l.ch)
	case '}':
		tok = newToken(RBRACE, l.ch)
	case '(':
		tok = newToken(LPAREN, l.ch)
	case ')':
		tok = newToken(RPAREN, l.ch)
	case ',':
		tok = newToken(COMMA, l.ch)
	case ';':
		tok = newToken(SEMICOLON, l.ch)
	case '"':
		tok.Type = STRING
		tok.Literal = l.readString()
	case 0:
		tok.Literal = ""
		tok.Type = EOF
	default:
		// If it's a letter, it's either an identifier or a keyword.
		if isLetter(l.ch) {
			literal := l.readIdentifier()
			tok.Type = LookupIdent(literal)
			tok.Literal = literal
			return tok
		} else if isDigit(l.ch) {
			tok.Type = NUMBER
			tok.Literal = l.readNumber()
			return tok
		} else {
			tok = Token{Type: ILLEGAL, Literal: string(l.ch)}
		}
	}

	l.readChar()
	return tok
}

func newToken(tokenType TokenType, ch rune) Token {
	return Token{Type: tokenType, Literal: string(ch)}
}

func (l *Lexer) skipWhitespace() {
	for unicode.IsSpace(l.ch) {
		l.readChar()
	}
}

func (l *Lexer) readIdentifier() string {
	position := l.position
	for isLetter(l.ch) || isDigit(l.ch) || l.ch == '_' || l.ch == '.' || l.ch == '"' {
		l.readChar()
	}
	return l.input[position:l.position]
}

func (l *Lexer) readNumber() string {
	position := l.position
	for isDigit(l.ch) {
		l.readChar()
	}
	return l.input[position:l.position]
}

func (l *Lexer) readString() string {
	// Skip the initial double quote.
	l.readChar()
	position := l.position
	for l.ch != '"' && l.ch != 0 {
		l.readChar()
	}
	s := l.input[position:l.position]
	// Skip closing double quote.
	return s
}

func isLetter(ch rune) bool {
	return unicode.IsLetter(ch)
}

func isDigit(ch rune) bool {
	return unicode.IsDigit(ch)
}

// ------------------------------------------------------
// AST (Abstract Syntax Tree) Nodes
// ------------------------------------------------------

// Migration holds the entire migration with Up and Down sections.
type Migration struct {
	Up   []Statement
	Down []Statement
}

// Statement represents a single DSL command.
type Statement interface {
	statementNode()
	String() string
}

// SQLStatement wraps a raw SQL command.
type SQLStatement struct {
	Command string
}

func (s *SQLStatement) statementNode() {}

func (s *SQLStatement) String() string {
	return strings.TrimSpace(s.Command)
}

// ------------------------------------------------------
// Extended AST Nodes for Schema Definitions
// ------------------------------------------------------

type ColumnDefinition struct {
	Name        string
	DataType    string
	Length      string   // optional length for data types
	Precision   string   // optional precision for numeric types
	Constraints []string // constraints like PRIMARY KEY, NOT NULL, etc.
	Comment     string   // optional comment
	Mapping     string   // optional mapping information
}

type CreateTableStatement struct {
	TableName   string
	Columns     []ColumnDefinition
	Constraints []string // indexes, foreign keys, etc. (for simplicity as raw strings)
}

func (cts *CreateTableStatement) statementNode() {}

func (cts *CreateTableStatement) String() string {
	cols := []string{}
	for _, col := range cts.Columns {
		s := fmt.Sprintf("%s %s", col.Name, col.DataType)
		if col.Length != "" {
			s += "(" + col.Length
			if col.Precision != "" {
				s += "," + col.Precision
			}
			s += ")"
		}
		if len(col.Constraints) > 0 {
			s += " " + strings.Join(col.Constraints, " ")
		}
		if col.Comment != "" {
			s += " COMMENT '" + col.Comment + "'"
		}
		if col.Mapping != "" {
			s += " MAPPED AS " + col.Mapping
		}
		cols = append(cols, s)
	}
	return fmt.Sprintf("CREATE TABLE %s (\n  %s\n)", cts.TableName, strings.Join(cols, ",\n  "))
}

type AlterTableStatement struct {
	Command string // For brevity, we wrap full alter-table commands.
}

func (ats *AlterTableStatement) statementNode() {}

func (ats *AlterTableStatement) String() string {
	return strings.TrimSpace(ats.Command)
}

// ------------------------------------------------------
// Parser
// ------------------------------------------------------

// Parser holds the state of the parser.
type Parser struct {
	l       *Lexer
	curTok  Token
	peekTok Token
	errors  []string
}

func NewParser(l *Lexer) *Parser {
	p := &Parser{l: l}
	// Read two tokens to initialize curTok and peekTok.
	p.nextToken()
	p.nextToken()
	return p
}

func (p *Parser) nextToken() {
	p.curTok = p.peekTok
	p.peekTok = p.l.NextToken()
}

// ParseMigration parses the entire input into a Migration struct.
func (p *Parser) ParseMigration() *Migration {
	migration := &Migration{}

	for p.curTok.Type != EOF {
		switch p.curTok.Type {
		case TokenType("UP"):
			p.nextToken() // move past the "Up" keyword
			migration.Up = p.parseBlock()
		case TokenType("DOWN"):
			p.nextToken() // move past the "Down" keyword
			migration.Down = p.parseBlock()
		default:
			p.nextToken() // skip any unexpected tokens
		}
	}

	return migration
}

// parseBlock expects a block enclosed by { and } and returns a slice of statements.
func (p *Parser) parseBlock() []Statement {
	stmts := []Statement{}

	if p.curTok.Type != LBRACE {
		p.errors = append(p.errors, "expected '{' at beginning of block")
		return stmts
	}

	p.nextToken() // skip '{'

	for p.curTok.Type != RBRACE && p.curTok.Type != EOF {
		stmt := p.parseStatement()
		if stmt != nil {
			stmts = append(stmts, stmt)
		}
		// If a semicolon is present, skip it.
		if p.curTok.Type == SEMICOLON {
			p.nextToken()
		}
	}
	// Skip the closing '}'
	if p.curTok.Type == RBRACE {
		p.nextToken()
	}
	return stmts
}

func (p *Parser) expectPeek(t TokenType) bool {
	if p.peekTok.Type == t {
		p.nextToken()
		return true
	}
	p.errors = append(p.errors, fmt.Sprintf("expected next token to be %s, got %s instead", t, p.peekTok.Type))
	return false
}

// Updated parseStatement: dispatch if command is CREATE TABLE or ALTER TABLE.
func (p *Parser) parseStatement() Statement {
	// Check for CREATE TABLE
	if strings.ToLower(p.curTok.Literal) == "create" && strings.ToLower(p.peekTok.Literal) == "table" {
		return p.parseCreateTableStatement()
	} else if strings.ToLower(p.curTok.Literal) == "alter" && strings.ToLower(p.peekTok.Literal) == "table" {
		return p.parseAlterTableStatement()
	} else if lower := strings.ToLower(p.curTok.Literal); lower == "select" || lower == "update" || lower == "delete" || lower == "insert" {
		// Parse a DML statement until semicolon, RBRACE, or EOF.
		var parts []string
		for p.curTok.Type != SEMICOLON && p.curTok.Type != RBRACE && p.curTok.Type != EOF {
			parts = append(parts, p.curTok.Literal)
			p.nextToken()
		}
		return &SQLStatement{Command: strings.Join(parts, " ")}
	}
	// ...existing raw SQL statement parsing...
	var parts []string
	// Skip stray semicolons.
	for p.curTok.Type == SEMICOLON {
		p.nextToken()
	}
	if p.curTok.Type == RBRACE || p.curTok.Type == EOF {
		return nil
	}
	for p.curTok.Type != SEMICOLON && p.curTok.Type != RBRACE && p.curTok.Type != EOF {
		parts = append(parts, p.curTok.Literal)
		p.nextToken()
	}
	command := strings.Join(parts, " ")
	return &SQLStatement{Command: command}
}

// Add helper to trim surrounding quotes from identifiers.
func trimQuotes(s string) string {
	if len(s) >= 2 && ((s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '`' && s[len(s)-1] == '`')) {
		return s[1 : len(s)-1]
	}
	return s
}

func (p *Parser) parseCreateTableStatement() Statement {
	// current token: "create", next: "table"
	p.nextToken() // skip "create"
	p.nextToken() // skip "table"
	// Next token: table name (allow quoted identifier)
	tableName := p.curTok.Literal
	tableName = trimQuotes(tableName) // <-- New: trim quotes
	p.nextToken()
	if p.curTok.Type != LPAREN {
		p.errors = append(p.errors, "expected '(' after table name")
		return nil
	}
	p.nextToken() // skip '('
	var columns []ColumnDefinition
	// Parse column definitions separated by comma until RPAREN
	for p.curTok.Type != RPAREN && p.curTok.Type != EOF {
		col := p.parseColumnDefinition()
		if col != nil {
			columns = append(columns, *col)
		}
		// If a comma is present, skip it.
		if p.curTok.Type == COMMA {
			p.nextToken()
		} else if p.curTok.Type == IDENT || p.curTok.Type == STRING {
			// Auto-handle missing comma, so continue without error.
			// (No step needed here; the loop will continue.)
		}
	}
	if p.curTok.Type == RPAREN {
		p.nextToken() // skip ')'
	}
	return &CreateTableStatement{
		TableName: tableName,
		Columns:   columns,
	}
}

func (p *Parser) parseColumnDefinition() *ColumnDefinition {
	col := &ColumnDefinition{}
	// Allow identifier to be a quoted string.
	if p.curTok.Type != IDENT && p.curTok.Type != STRING {
		p.errors = append(p.errors, "expected column name")
		return nil
	}
	col.Name = trimQuotes(p.curTok.Literal) // <-- New: trim quotes on column name
	p.nextToken()
	if p.curTok.Type != IDENT && p.curTok.Type != STRING {
		p.errors = append(p.errors, "expected data type for column "+col.Name)
		return nil
	}
	col.DataType = p.curTok.Literal
	p.nextToken()
	// Optional: length/precision (e.g., varchar(255) or decimal(10,2))
	if p.curTok.Type == LPAREN {
		p.nextToken() // skip '('
		if p.curTok.Type == NUMBER || p.curTok.Type == IDENT {
			col.Length = p.curTok.Literal
			p.nextToken()
			if p.curTok.Type == COMMA {
				p.nextToken()
				if p.curTok.Type == NUMBER || p.curTok.Type == IDENT {
					col.Precision = p.curTok.Literal
					p.nextToken()
				}
			}
		}
		if p.curTok.Type == RPAREN {
			p.nextToken() // skip ')'
		}
	}
	// Parse additional constraints, comments, or mappings until a comma or RPAREN.
	for p.curTok.Type != COMMA && p.curTok.Type != RPAREN && p.curTok.Type != EOF {
		lit := strings.ToLower(p.curTok.Literal)
		if lit == "comment" {
			p.nextToken()
			if p.curTok.Type == STRING {
				col.Comment = p.curTok.Literal
				p.nextToken()
				continue
			}
		}
		if lit == "mapped" {
			p.nextToken()
			if strings.ToLower(p.curTok.Literal) == "as" {
				p.nextToken()
				if p.curTok.Type == STRING || p.curTok.Type == IDENT {
					col.Mapping = p.curTok.Literal
					p.nextToken()
					continue
				}
			}
		}
		// Otherwise treat token as part of column constraints.
		col.Constraints = append(col.Constraints, p.curTok.Literal)
		p.nextToken()
	}
	return col
}

func (p *Parser) parseAlterTableStatement() Statement {
	// For now, capture full alter table command as raw SQL.
	var parts []string
	for p.curTok.Type != SEMICOLON && p.curTok.Type != RBRACE && p.curTok.Type != EOF {
		parts = append(parts, p.curTok.Literal)
		p.nextToken()
	}
	command := strings.Join(parts, " ")
	return &AlterTableStatement{Command: command}
}

// Errors returns a slice of parser errors.
func (p *Parser) Errors() []string {
	return p.errors
}

// ------------------------------------------------------
// SQL Generator (Visitor Pattern)
// ------------------------------------------------------

// GenerateSQL simply returns the SQL commands from the list of statements.
func GenerateSQL(stmts []Statement) []string {
	queries := []string{}
	for _, stmt := range stmts {
		queries = append(queries, stmt.String())
	}
	return queries
}

// Add driver-specific SQL generation functions
func appendDelimiter(driver, command string) string {
	switch strings.ToLower(driver) {
	case "sqlserver":
		if !strings.HasSuffix(strings.TrimSpace(command), "GO") {
			return command + "\nGO"
		}
		return command
	default:
		if !strings.HasSuffix(strings.TrimSpace(command), ";") {
			return command + ";"
		}
		return command
	}
}

func convertStatement(driver, command string) string {
	converted := command
	switch strings.ToLower(driver) {
	case "postgres":
		converted = strings.ReplaceAll(converted, "autoincrement", "SERIAL")
	case "mysql":
		converted = strings.ReplaceAll(converted, "autoincrement", "AUTO_INCREMENT")
	case "sqlserver":
		converted = strings.ReplaceAll(converted, "autoincrement", "IDENTITY(1,1)")
	}
	return appendDelimiter(driver, converted)
}

// ------------------------------------------------------
// Generic Data Type Mapping for Migration (Updated)
// ------------------------------------------------------
func mapGenericDataType(driver string, col ColumnDefinition) string {
	typ := strings.ToLower(col.DataType)
	switch typ {
	case "string":
		if col.Length != "" {
			return "string(" + col.Length + ")"
		}
		return "string"
	case "number":
		if col.Precision != "" {
			l := col.Length
			if l == "" {
				l = "11"
			}
			return "number(" + l + "," + col.Precision + ")"
		}
		if col.Length != "" {
			return "number(" + col.Length + ")"
		}
		return "number"
	default:
		// For other types (such as integer, boolean, float, etc.), return as DSL types.
		if col.Length != "" {
			if col.Precision != "" {
				return typ + "(" + col.Length + "," + col.Precision + ")"
			}
			return typ + "(" + col.Length + ")"
		}
		return typ
	}
}

func (cts *CreateTableStatement) ToSQL(driver string) string {
	var colDefs []string
	for _, col := range cts.Columns {
		colDefs = append(colDefs, generateColumnSQL(driver, col))
	}
	return fmt.Sprintf("CREATE TABLE %s (\n  %s\n)", cts.TableName, strings.Join(colDefs, ",\n  "))
}

// Update the driver-specific SQL generation to use the new mapping in create table statements.
func GenerateSQLForDriver(driver string, stmts []Statement) []string {
	var queries []string
	for _, stmt := range stmts {
		switch s := stmt.(type) {
		case *CreateTableStatement:
			queries = append(queries, appendDelimiter(driver, s.ToSQL(driver)))
		default:
			queries = append(queries, convertStatement(driver, stmt.String()))
		}
	}
	return queries
}

// ------------------------------------------------------
// Production-Ready Column SQL Generation
// ------------------------------------------------------
// New helper to generate driver-specific column definitions.
func generateColumnSQL(driver string, col ColumnDefinition) string {
	// Detect autoincrement and primary key based on constraints.
	auto := false
	primary := false
	var newConstraints []string
	for _, c := range col.Constraints {
		lc := strings.ToLower(c)
		if lc == "autoincrement" {
			auto = true
			continue
		}
		if lc == "primary" || lc == "primary key" {
			primary = true
		}
		newConstraints = append(newConstraints, c)
	}
	var mappedType string
	typ := strings.ToLower(col.DataType)
	switch typ {
	case "string":
		// Default length is 255 if unspecified.
		length := col.Length
		if length == "" {
			length = "255"
		}
		switch strings.ToLower(driver) {
		case "postgres", "mysql":
			mappedType = "VARCHAR(" + length + ")"
		case "sqlite":
			mappedType = "TEXT"
		case "sqlserver":
			mappedType = "NVARCHAR(" + length + ")"
		default:
			mappedType = "VARCHAR(" + length + ")"
		}
	case "integer":
		if auto {
			switch strings.ToLower(driver) {
			case "postgres":
				mappedType = "SERIAL"
			case "mysql":
				mappedType = "INT AUTO_INCREMENT"
			case "sqlite":
				// For SQLite, a primary key autoincrement column must be declared as below.
				if primary {
					return col.Name + " INTEGER PRIMARY KEY AUTOINCREMENT"
				}
				mappedType = "INTEGER"
			case "sqlserver":
				mappedType = "INT IDENTITY(1,1)"
			default:
				mappedType = "INTEGER"
			}
		} else {
			mappedType = "INTEGER"
		}
	case "number":
		if col.Precision != "" {
			l := col.Length
			if l == "" {
				l = "11"
			}
			mappedType = "DECIMAL(" + l + "," + col.Precision + ")"
		} else if col.Length != "" {
			mappedType = "DECIMAL(" + col.Length + ")"
		} else {
			mappedType = "DECIMAL"
		}
	case "float":
		mappedType = "FLOAT"
	case "boolean":
		mappedType = "BOOLEAN"
	case "date":
		mappedType = "DATE"
	case "datetime":
		mappedType = "DATETIME"
	case "datetimetz":
		mappedType = "TIMESTAMPTZ"
	case "decimal":
		if col.Precision != "" {
			mappedType = "DECIMAL(" + col.Length + "," + col.Precision + ")"
		} else if col.Length != "" {
			mappedType = "DECIMAL(" + col.Length + ")"
		} else {
			mappedType = "DECIMAL"
		}
	case "money":
		switch strings.ToLower(driver) {
		case "postgres":
			mappedType = "MONEY"
		case "mysql":
			mappedType = "DECIMAL(19,4)"
		default:
			mappedType = "DECIMAL(19,4)"
		}
	default:
		mappedType = col.DataType
	}
	// Build final column SQL.
	sqlDef := col.Name + " " + mappedType
	// Append constraints (except autoincrement, already reflected in type).
	if len(newConstraints) > 0 {
		sqlDef += " " + strings.Join(newConstraints, " ")
	}
	if col.Comment != "" {
		sqlDef += " COMMENT '" + col.Comment + "'"
	}
	if col.Mapping != "" {
		sqlDef += " MAPPED AS " + col.Mapping
	}
	return sqlDef
}

// ------------------------------------------------------
// Main: Example usage
// ------------------------------------------------------

func main() {
	input := `
Up {
    create table "test" (
        id integer primary key autoincrement,
        name string,
        age integer(3) not null comment "user age" mapped as "age_db"
    );
    create table "test2" (
        id integer primary key autoincrement,
        description string(255) comment "description of test2"
        name string comment "description of test2"
    );
    insert into "test" (name) values ('test');
    insert into "test2" (description) values ('test2');
    alter table "test" rename to "test3";
    alter table "test" ( add column id integer primary key autoincrement, drop column name, change column id boolean );
}

Down {
    alter table "test" ( drop column id, drop column name )
    alter table "test3" rename to "test";
    drop table "test3";
    drop table "test2";
}
`
	// Create lexer and parser.
	lexer := NewLexer(input)
	parser := NewParser(lexer)
	migration := parser.ParseMigration()

	// Report any parser errors.
	if len(parser.Errors()) > 0 {
		fmt.Println("Parser errors:")
		for _, err := range parser.Errors() {
			fmt.Println(" -", err)
		}
		return
	}

	// Generate and print generic SQL for Up and Down sections.
	fmt.Println("---- UP SQL Statements ----")
	upSQL := GenerateSQL(migration.Up)
	for _, q := range upSQL {
		fmt.Println(q)
	}

	fmt.Println("\n---- DOWN SQL Statements ----")
	downSQL := GenerateSQL(migration.Down)
	for _, q := range downSQL {
		fmt.Println(q)
	}

	// Generate and print driver-specific SQL statements.
	drivers := []string{"postgres", "mysql", "sqlite", "sqlserver"}
	for _, driver := range drivers {
		fmt.Println("\n----", strings.ToUpper(driver), "UP SQL Statements ----")
		upDrvSQL := GenerateSQLForDriver(driver, migration.Up)
		for _, q := range upDrvSQL {
			fmt.Println(q)
		}
		fmt.Println("\n----", strings.ToUpper(driver), "DOWN SQL Statements ----")
		downDrvSQL := GenerateSQLForDriver(driver, migration.Down)
		for _, q := range downDrvSQL {
			fmt.Println(q)
		}
	}
}
