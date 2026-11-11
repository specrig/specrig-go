package harness

type dialect struct {
	Name         string
	Type         string // sqltest's type, App's field and the interface a step gets
	Constructor  string
	Image        string // empty for a database that needs no server
	RefuseCommit bool
}

var (
	postgresDialect = &dialect{Name: "postgres", Type: "Postgres", Constructor: "New", Image: "postgres:18-alpine", RefuseCommit: true}
	mysqlDialect    = &dialect{Name: "mysql", Type: "Mysql", Constructor: "NewMysql", Image: "mysql:8.4"}
	sqliteDialect   = &dialect{Name: "sqlite", Type: "Sqlite", Constructor: "NewSqlite"}
)

func dialectOf(driver string) *dialect {
	switch driver {
	case "", "postgres", "pgx", "pgsql":
		return postgresDialect
	case "mysql":
		return mysqlDialect
	case "sqlite", "sqlite3":
		return sqliteDialect
	}
	return nil
}
