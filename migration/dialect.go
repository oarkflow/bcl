package migration

type Dialect interface {
	CreateTableSQL(ct CreateTable, up bool) (string, error)
	RenameTableSQL(rt RenameTable) (string, error)
	DeleteDataSQL(dd DeleteData) (string, error)
	DropEnumTypeSQL(de DropEnumType) (string, error)
	DropRowPolicySQL(drp DropRowPolicy) (string, error)
	DropMaterializedViewSQL(dmv DropMaterializedView) (string, error)
	DropTableSQL(dt DropTable) (string, error)
	DropSchemaSQL(ds DropSchema) (string, error)
	AddColumnSQL(ac AddColumn, tableName string) ([]string, error)
	DropColumnSQL(dc DropColumn, tableName string) (string, error)
	RenameColumnSQL(rc RenameColumn, tableName string) (string, error)
	MapDataType(genericType string, size int, autoIncrement, primaryKey bool) string
	CreateViewSQL(cv CreateView) (string, error)
	DropViewSQL(dv DropView) (string, error)
	RenameViewSQL(rv RenameView) (string, error)
	CreateFunctionSQL(cf CreateFunction) (string, error)
	DropFunctionSQL(df DropFunction) (string, error)
	RenameFunctionSQL(rf RenameFunction) (string, error)
	CreateProcedureSQL(cp CreateProcedure) (string, error)
	DropProcedureSQL(dp DropProcedure) (string, error)
	RenameProcedureSQL(rp RenameProcedure) (string, error)
	CreateTriggerSQL(ct CreateTrigger) (string, error)
	DropTriggerSQL(dt DropTrigger) (string, error)
	RenameTriggerSQL(rt RenameTrigger) (string, error)
	WrapInTransaction(queries []string) []string
	WrapInTransactionWithConfig(queries []string, trans Transaction) []string
	InsertSQL(table string, columns []string, values []any) (string, error)
	TableExistsSQL(table string) string
	EOS() string
}

var dialectRegistry = map[string]Dialect{}

func init() {
	dialectRegistry[DialectPostgres] = &PostgresDialect{}
	dialectRegistry[DialectMySQL] = &MySQLDialect{}
	dialectRegistry[DialectSQLite] = &SQLiteDialect{}
}

func getDialect(name string) Dialect {
	if d, ok := dialectRegistry[name]; ok {
		return d
	}
	return dialectRegistry[DialectPostgres]
}
