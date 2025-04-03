package main

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// -------------------------
// Token Definitions
// -------------------------

type TokenType string

const (
	ILLEGAL   TokenType = "ILLEGAL"
	EOF                 = "EOF"
	IDENT               = "IDENT"
	NUMBER              = "NUMBER"
	STRING              = "STRING"
	COMMA               = ","
	SEMICOLON           = ";"
	LPAREN              = "("
	RPAREN              = ")"
	LBRACE              = "{"
	RBRACE              = "}"
	// migration commands
	UP   TokenType = "UP"
	DOWN TokenType = "DOWN"
	// SQL commands
	SELECT TokenType = "SELECT"
	UPDATE TokenType = "UPDATE"
	DELETE TokenType = "DELETE"
	INSERT TokenType = "INSERT"
	CREATE TokenType = "CREATE"
	ALTER  TokenType = "ALTER"
	DROP   TokenType = "DROP"
	RENAME TokenType = "RENAME"
	ADD    TokenType = "ADD"
	CHANGE TokenType = "CHANGE"
)

// Additional keywords
var keywords = map[string]TokenType{
	"up":         UP,
	"down":       DOWN,
	"select":     SELECT,
	"update":     UPDATE,
	"delete":     DELETE,
	"insert":     INSERT,
	"create":     CREATE,
	"table":      IDENT, // table name is handled in parser; keyword here only matters in context.
	"alter":      ALTER,
	"drop":       DROP,
	"rename":     RENAME,
	"add":        ADD,
	"change":     CHANGE,
	"foreign":    IDENT, // for foreign key constraint
	"key":        IDENT,
	"constraint": IDENT,
	"references": IDENT,
}

type Token struct {
	Type    TokenType
	Literal string
	Line    int
	Column  int
}

func LookupIdent(ident string) TokenType {
	lower := strings.ToLower(ident)
	if tok, ok := keywords[lower]; ok {
		return tok
	}
	return IDENT
}

// -------------------------
// Lexer Implementation
// -------------------------

type Lexer struct {
	input        string
	position     int  // current position in input (points to current char)
	readPosition int  // current reading position in input (after current char)
	ch           rune // current char under examination
	line         int
	column       int
}

func NewLexer(input string) *Lexer {
	l := &Lexer{
		input:  input,
		line:   1,
		column: 0,
	}
	l.readChar()
	return l
}

func (l *Lexer) readChar() {
	if l.readPosition >= len(l.input) {
		l.ch = 0
		return
	}
	r, size := utf8.DecodeRuneInString(l.input[l.readPosition:])
	l.ch = r
	l.position = l.readPosition
	l.readPosition += size

	// Update line and column counters.
	if l.ch == '\n' {
		l.line++
		l.column = 0
	} else {
		l.column++
	}
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
	l.skipWhitespaceAndComments()
	tok.Line = l.line
	tok.Column = l.column

	switch l.ch {
	case '{':
		tok = newToken(LBRACE, l.ch, l.line, l.column)
	case '}':
		tok = newToken(RBRACE, l.ch, l.line, l.column)
	case '(':
		tok = newToken(LPAREN, l.ch, l.line, l.column)
	case ')':
		tok = newToken(RPAREN, l.ch, l.line, l.column)
	case ',':
		tok = newToken(COMMA, l.ch, l.line, l.column)
	case ';':
		tok = newToken(SEMICOLON, l.ch, l.line, l.column)
	case '"', '\'':
		quote := l.ch
		tok.Type = STRING
		tok.Literal = l.readString(quote)
		tok.Line = l.line
		tok.Column = l.column
		// **Return immediately so that we don't call l.readChar() again.**
		return tok
	case 0:
		tok.Literal = ""
		tok.Type = EOF
	default:
		if isLetter(l.ch) || l.ch == '_' {
			startLine, startCol := l.line, l.column
			literal := l.readIdentifier()
			tok.Type = LookupIdent(literal)
			tok.Literal = literal
			tok.Line = startLine
			tok.Column = startCol
			return tok
		} else if isDigit(l.ch) {
			startLine, startCol := l.line, l.column
			tok.Type = NUMBER
			tok.Literal = l.readNumber()
			tok.Line = startLine
			tok.Column = startCol
			return tok
		} else {
			tok = Token{Type: ILLEGAL, Literal: string(l.ch), Line: l.line, Column: l.column}
		}
	}
	l.readChar()
	return tok
}

func newToken(tokenType TokenType, ch rune, line, col int) Token {
	return Token{Type: tokenType, Literal: string(ch), Line: line, Column: col}
}

func (l *Lexer) skipWhitespaceAndComments() {
	for {
		// Skip whitespace characters.
		for unicode.IsSpace(l.ch) {
			l.readChar()
		}
		// Skip comments: support -- and /* */
		if l.ch == '-' && l.peekChar() == '-' {
			l.skipLineComment()
			continue
		}
		if l.ch == '/' && l.peekChar() == '*' {
			l.skipBlockComment()
			continue
		}
		break
	}
}

func (l *Lexer) skipLineComment() {
	for l.ch != '\n' && l.ch != 0 {
		l.readChar()
	}
}

func (l *Lexer) skipBlockComment() {
	// consume "/*"
	l.readChar() // '/'
	l.readChar() // '*'
	for {
		if l.ch == 0 {
			break
		}
		if l.ch == '*' && l.peekChar() == '/' {
			l.readChar() // '*'
			l.readChar() // '/'
			break
		}
		l.readChar()
	}
}

func (l *Lexer) readIdentifier() string {
	position := l.position
	for isLetter(l.ch) || isDigit(l.ch) || l.ch == '_' || l.ch == '.' {
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

func (l *Lexer) readString(quote rune) string {
	// Skip opening quote.
	l.readChar()
	var sb strings.Builder
	for {
		if l.ch == quote {
			break
		}
		if l.ch == '\\' { // handle escapes
			l.readChar()
			switch l.ch {
			case 'n':
				sb.WriteRune('\n')
			case 't':
				sb.WriteRune('\t')
			case 'r':
				sb.WriteRune('\r')
			case '\\':
				sb.WriteRune('\\')
			case quote:
				sb.WriteRune(quote)
			default:
				sb.WriteRune(l.ch)
			}
		} else if l.ch == 0 {
			break
		} else {
			sb.WriteRune(l.ch)
		}
		l.readChar()
	}
	result := sb.String()
	// Skip closing quote.
	l.readChar()
	return result
}

func isLetter(ch rune) bool {
	return unicode.IsLetter(ch)
}

func isDigit(ch rune) bool {
	return unicode.IsDigit(ch)
}

// -------------------------
// AST and Parser Definitions
// -------------------------

// Statement represents a migration statement.
type Statement interface {
	statementNode()
	String() string
}

// Migration holds the Up and Down blocks.
type Migration struct {
	Up          []Statement
	Down        []Statement
	ParseErrors []string
}

// SQLStatement is a generic SQL command.
type SQLStatement struct {
	Command string
}

func (s *SQLStatement) statementNode() {}
func (s *SQLStatement) String() string {
	return strings.TrimSpace(s.Command)
}

// ColumnDefinition holds column information.
type ColumnDefinition struct {
	Name        string
	DataType    string
	Length      string
	Precision   string
	Constraints []string
	Comment     string
	Mapping     string
	// Foreign key clause (optional): "REFERENCES <table>(<col>)"
	ForeignKey string
}

// CreateTableStatement represents a CREATE TABLE command.
type CreateTableStatement struct {
	TableName        string
	Columns          []ColumnDefinition
	TableConstraints []string // e.g., primary key, foreign keys defined at table level.
}

func (cts *CreateTableStatement) statementNode() {}
func (cts *CreateTableStatement) String() string {
	cols := []string{}
	for _, col := range cts.Columns {
		s := generateColumnSQL("generic", col)
		cols = append(cols, s)
	}
	all := strings.Join(cols, ",\n  ")
	if len(cts.TableConstraints) > 0 {
		all += ",\n  " + strings.Join(cts.TableConstraints, ",\n  ")
	}
	return fmt.Sprintf("CREATE TABLE %s (\n  %s\n)", cts.TableName, all)
}

// AlterTableStatement represents an ALTER TABLE command.
type AlterTableStatement struct {
	Command string
}

func (ats *AlterTableStatement) statementNode() {}
func (ats *AlterTableStatement) String() string {
	return strings.TrimSpace(ats.Command)
}

// Parser holds state for parsing tokens.
type Parser struct {
	l       *Lexer
	curTok  Token
	peekTok Token
	errors  []string
}

func NewParser(l *Lexer) *Parser {
	p := &Parser{
		l:      l,
		errors: []string{},
	}
	p.nextToken()
	p.nextToken()
	return p
}

func (p *Parser) nextToken() {
	p.curTok = p.peekTok
	p.peekTok = p.l.NextToken()
}

func (p *Parser) addError(msg string, tok Token) {
	err := fmt.Sprintf("[Line %d, Col %d] %s", tok.Line, tok.Column, msg)
	p.errors = append(p.errors, err)
}

func (p *Parser) Errors() []string {
	return p.errors
}

// ParseMigration parses the entire migration file.
func (p *Parser) ParseMigration() *Migration {
	migration := &Migration{}
	for p.curTok.Type != EOF {
		switch LookupIdent(p.curTok.Literal) {
		case UP:
			p.nextToken()
			migration.Up = p.parseBlock()
		case DOWN:
			p.nextToken()
			migration.Down = p.parseBlock()
		default:
			// Skip tokens until we find a block marker.
			p.nextToken()
		}
	}
	migration.ParseErrors = p.errors
	return migration
}

// parseBlock parses a block delimited by braces.
func (p *Parser) parseBlock() []Statement {
	stmts := []Statement{}
	if p.curTok.Type != LBRACE {
		p.addError("expected '{' at beginning of block", p.curTok)
		return stmts
	}
	p.nextToken()
	for p.curTok.Type != RBRACE && p.curTok.Type != EOF {
		stmt := p.parseStatement()
		if stmt != nil {
			stmts = append(stmts, stmt)
		} else {
			// Attempt error recovery: skip until semicolon or RBRACE.
			for p.curTok.Type != SEMICOLON && p.curTok.Type != RBRACE && p.curTok.Type != EOF {
				p.nextToken()
			}
		}
		if p.curTok.Type == SEMICOLON {
			p.nextToken()
		}
	}
	if p.curTok.Type == RBRACE {
		p.nextToken()
	}
	return stmts
}

// parseStatement decides what type of statement is next.
func (p *Parser) parseStatement() Statement {
	// Handle specific commands.
	literalLower := strings.ToLower(p.curTok.Literal)
	if literalLower == "create" && strings.ToLower(p.peekTok.Literal) == "table" {
		return p.parseCreateTableStatement()
	} else if literalLower == "alter" && strings.ToLower(p.peekTok.Literal) == "table" {
		return p.parseAlterTableStatement()
	} else if literalLower == "select" || literalLower == "update" ||
		literalLower == "delete" || literalLower == "insert" {
		return p.parseSQLStatement()
	}
	// Fallback: treat as generic SQL.
	return p.parseSQLStatement()
}

// parseSQLStatement parses a generic SQL statement.
func (p *Parser) parseSQLStatement() Statement {
	var parts []string
	for p.curTok.Type != SEMICOLON && p.curTok.Type != RBRACE && p.curTok.Type != EOF {
		parts = append(parts, p.curTok.Literal)
		p.nextToken()
	}
	command := strings.Join(parts, " ")
	return &SQLStatement{Command: command}
}

// parseCreateTableStatement parses a CREATE TABLE command.
func (p *Parser) parseCreateTableStatement() Statement {
	// consume "create"
	p.nextToken()
	// consume "table"
	p.nextToken()
	tableName := trimQuotes(p.curTok.Literal)
	p.nextToken()
	// Expect LPAREN
	if p.curTok.Type != LPAREN {
		p.addError("expected '(' after table name", p.curTok)
		return nil
	}
	p.nextToken()

	columns := []ColumnDefinition{}
	tableConstraints := []string{}
	// Parse column definitions and/or table constraints until RPAREN.
	for p.curTok.Type != RPAREN && p.curTok.Type != EOF {
		// If token "constraint" or known table constraint keywords appear, parse table constraint.
		if strings.ToLower(p.curTok.Literal) == "constraint" ||
			(strings.ToLower(p.curTok.Literal) == "foreign" && strings.ToLower(p.peekTok.Literal) == "key") {
			tc := p.parseTableConstraint()
			if tc != "" {
				tableConstraints = append(tableConstraints, tc)
			}
		} else {
			col := p.parseColumnDefinition()
			if col != nil {
				columns = append(columns, *col)
			}
		}
		// If comma, consume and continue.
		if p.curTok.Type == COMMA {
			p.nextToken()
		}
	}
	if p.curTok.Type == RPAREN {
		p.nextToken()
	}
	return &CreateTableStatement{
		TableName:        tableName,
		Columns:          columns,
		TableConstraints: tableConstraints,
	}
}

// parseColumnDefinition parses one column definition.
func (p *Parser) parseColumnDefinition() *ColumnDefinition {
	col := &ColumnDefinition{}
	if p.curTok.Type != IDENT && p.curTok.Type != STRING {
		p.addError("expected column name", p.curTok)
		return nil
	}
	col.Name = trimQuotes(p.curTok.Literal)
	p.nextToken()
	if p.curTok.Type != IDENT && p.curTok.Type != STRING {
		p.addError(fmt.Sprintf("expected data type for column %s", col.Name), p.curTok)
		return nil
	}
	col.DataType = p.curTok.Literal
	p.nextToken()
	// Handle optional length/precision
	if p.curTok.Type == LPAREN {
		p.nextToken()
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
			p.nextToken()
		} else {
			p.addError("expected ')' after length/precision", p.curTok)
		}
	}
	// Parse constraints, comments, mapping and foreign key clause.
constraintLoop:
	for p.curTok.Type != COMMA && p.curTok.Type != RPAREN && p.curTok.Type != EOF {
		lit := strings.ToLower(p.curTok.Literal)
		switch lit {
		case "comment":
			p.nextToken()
			if p.curTok.Type == STRING {
				col.Comment = p.curTok.Literal
				p.nextToken()
				continue constraintLoop
			} else {
				p.addError("expected string after comment", p.curTok)
			}
		case "mapped":
			p.nextToken()
			if strings.ToLower(p.curTok.Literal) == "as" {
				p.nextToken()
				if p.curTok.Type == IDENT || p.curTok.Type == STRING {
					col.Mapping = p.curTok.Literal
					p.nextToken()
					continue constraintLoop
				} else {
					p.addError("expected mapping identifier after 'mapped as'", p.curTok)
				}
			}
		case "foreign":
			// Parse foreign key clause: expect: foreign key references <table>(<col>)
			p.nextToken() // skip 'foreign'
			if strings.ToLower(p.curTok.Literal) != "key" {
				p.addError("expected 'key' after 'foreign'", p.curTok)
			} else {
				p.nextToken() // skip 'key'
			}
			if strings.ToLower(p.curTok.Literal) != "references" {
				p.addError("expected 'references' in foreign key clause", p.curTok)
			} else {
				p.nextToken() // skip 'references'
			}
			// Read referenced table.
			refTable := trimQuotes(p.curTok.Literal)
			p.nextToken()
			// Expect LPAREN.
			if p.curTok.Type != LPAREN {
				p.addError("expected '(' after referenced table in foreign key clause", p.curTok)
				return col
			}
			p.nextToken() // skip LPAREN
			if p.curTok.Type != IDENT && p.curTok.Type != STRING {
				p.addError("expected referenced column name", p.curTok)
				return col
			}
			refCol := trimQuotes(p.curTok.Literal)
			p.nextToken()
			if p.curTok.Type != RPAREN {
				p.addError("expected ')' after referenced column in foreign key clause", p.curTok)
				return col
			}
			p.nextToken() // skip RPAREN
			col.ForeignKey = fmt.Sprintf("REFERENCES %s(%s)", refTable, refCol)
			break constraintLoop
		default:
			col.Constraints = append(col.Constraints, p.curTok.Literal)
			p.nextToken()
		}
	}
	return col
}

// parseTableConstraint parses a table-level constraint as a raw string.
func (p *Parser) parseTableConstraint() string {
	var parts []string
	for p.curTok.Type != COMMA && p.curTok.Type != RPAREN && p.curTok.Type != EOF {
		parts = append(parts, p.curTok.Literal)
		p.nextToken()
	}
	return strings.Join(parts, " ")
}

func trimQuotes(s string) string {
	if len(s) >= 2 && ((s[0] == '"' && s[len(s)-1] == '"') ||
		(s[0] == '\'' && s[len(s)-1] == '\'')) {
		return s[1 : len(s)-1]
	}
	return s
}

// parseAlterTableStatement parses an ALTER TABLE command.
func (p *Parser) parseAlterTableStatement() Statement {
	var parts []string
	for p.curTok.Type != SEMICOLON && p.curTok.Type != RBRACE && p.curTok.Type != EOF {
		parts = append(parts, p.curTok.Literal)
		p.nextToken()
	}
	command := strings.Join(parts, " ")
	return &AlterTableStatement{Command: command}
}

// -------------------------
// SQL Generation
// -------------------------

// generateColumnSQL converts a column definition to SQL for a given driver.
func generateColumnSQL(driver string, col ColumnDefinition) string {
	auto := false
	primary := false
	var constraints []string
	for _, c := range col.Constraints {
		lc := strings.ToLower(c)
		if lc == "autoincrement" {
			auto = true
			continue
		}
		if lc == "primary" || lc == "primary key" {
			primary = true
		}
		constraints = append(constraints, c)
	}
	var mappedType string
	typ := strings.ToLower(col.DataType)
	switch typ {
	case "string":
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
				if primary {
					return fmt.Sprintf("%s INTEGER PRIMARY KEY AUTOINCREMENT", col.Name)
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
	sqlDef := fmt.Sprintf("%s %s", col.Name, mappedType)
	if len(constraints) > 0 {
		sqlDef += " " + strings.Join(constraints, " ")
	}
	if col.Comment != "" && strings.ToLower(driver) != "postgres" {
		sqlDef += " COMMENT '" + col.Comment + "'"
	}
	if col.Mapping != "" {
		sqlDef += " MAPPED AS " + col.Mapping
	}
	if col.ForeignKey != "" {
		sqlDef += " " + col.ForeignKey
	}
	return sqlDef
}

// GenerateSQL converts a slice of statements to SQL strings.
func GenerateSQL(stmts []Statement) []string {
	queries := []string{}
	for _, stmt := range stmts {
		queries = append(queries, stmt.String())
	}
	return queries
}

// appendDelimiter adds the appropriate delimiter for a driver.
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

// convertStatement applies driver‑specific conversions.
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

// GenerateSQLForDriver generates SQL for a specific driver,
// wrapping migration blocks in transactions where appropriate.
func GenerateSQLForDriver(driver string, stmts []Statement) []string {
	var queries []string
	// Begin transaction wrapper if supported.
	switch strings.ToLower(driver) {
	case "postgres", "mysql":
		queries = append(queries, "BEGIN;")
	}
	for _, stmt := range stmts {
		switch s := stmt.(type) {
		case *CreateTableStatement:
			// Use driver-specific column mapping.
			queries = append(queries, appendDelimiter(driver, GenerateCreateTableSQLForDriver(driver, s)))
		default:
			queries = append(queries, convertStatement(driver, stmt.String()))
		}
	}
	// End transaction wrapper.
	switch strings.ToLower(driver) {
	case "postgres", "mysql":
		queries = append(queries, "COMMIT;")
	}
	return queries
}

// GenerateCreateTableSQLForDriver converts a CreateTableStatement for a given driver.
func GenerateCreateTableSQLForDriver(driver string, s *CreateTableStatement) string {
	var colDefs []string
	for _, col := range s.Columns {
		colDefs = append(colDefs, generateColumnSQL(driver, col))
	}
	all := strings.Join(colDefs, ",\n  ")
	if len(s.TableConstraints) > 0 {
		all += ",\n  " + strings.Join(s.TableConstraints, ",\n  ")
	}
	return fmt.Sprintf("CREATE TABLE %s (\n  %s\n)", s.TableName, all)
}

// -------------------------
// Main Function and Demo
// -------------------------

func main() {
	input := `
-- Migration example with enhanced syntax and comments.
Up {
    create table "users" (
        id integer primary key autoincrement,
        username string(50) not null,
        email string(100) not null,
        age integer,
        -- Foreign key example inline:
        group_id integer foreign key references "groups"(id),
        comment "User information table"
    );
    create table "groups" (
        id integer primary key autoincrement,
        name string(100) not null,
        comment "User group table",
        constraint pk_groups primary key (id),
        constraint fk_group_users foreign key (id) references "users"(group_id)
    );
    insert into "users" (username, email) values ('alice', 'alice@example.com');
    alter table "users" rename to "app_users";
}

Down {
    alter table "app_users" rename to "users";
    drop table "users";
    drop table "groups";
}
`
	lexer := NewLexer(input)
	parser := NewParser(lexer)
	migration := parser.ParseMigration()

	if len(migration.ParseErrors) > 0 {
		fmt.Println("Parser errors:")
		for _, err := range migration.ParseErrors {
			fmt.Println(" -", err)
		}
		return
	}

	// Display raw migration SQL commands.
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

	// Generate driver-specific SQL.
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
