module github.com/pgedge/ai-workbench/server

go 1.24.0

require (
	github.com/jackc/pgx/v5 v5.7.6
	github.com/mattn/go-sqlite3 v1.14.32
	github.com/pgedge/ai-workbench/pkg v0.0.0
	golang.org/x/crypto v0.47.0
	golang.org/x/sync v0.19.0
	golang.org/x/term v0.39.0
	gopkg.in/yaml.v3 v3.0.1
)

replace github.com/pgedge/ai-workbench/pkg => ../../pkg

require (
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/jackc/puddle/v2 v2.2.2 // indirect
	github.com/kr/text v0.2.0 // indirect
	github.com/rogpeppe/go-internal v1.14.1 // indirect
	golang.org/x/sys v0.40.0 // indirect
	golang.org/x/text v0.33.0 // indirect
)
