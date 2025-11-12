package main

import (
	"bufio"
	"database/sql"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	_ "github.com/jackc/pgx/v5/stdlib"

	"nfe-drop/internal/config"
	"nfe-drop/internal/migrations"
)

func main() {
	// Flags:
	// --auto  => modo não interativo (para Ansible)
	// --force => só faz sentido em modo manual: dropa e recria DB existente
	auto := flag.Bool("auto", false, "modo automático (não interativo) para automação; cria DB se não existir, roda migrations, NUNCA dropa DB existente")
	force := flag.Bool("force", false, "força drop e recriação do banco se ele já existir (modo manual)")
	flag.Parse()

	log.Println("[nfe-drop-migrator] iniciando...")

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("erro carregando configuração: %v", err)
	}

	// Conecta no banco admin (postgres)
	adminDB, err := sql.Open("pgx", cfg.AdminDSN())
	if err != nil {
		log.Fatalf("erro conectando ao Postgres (admin): %v", err)
	}
	defer adminDB.Close()

	if err := adminDB.Ping(); err != nil {
		log.Fatalf("erro no ping ao Postgres (admin): %v", err)
	}

	log.Printf("Conectado ao Postgres admin em %s:%d\n", cfg.DBHost, cfg.DBPort)

	// Verifica existência do DB da aplicação
	exists, err := databaseExists(adminDB, cfg.DBName)
	if err != nil {
		log.Fatalf("erro verificando existência do banco %q: %v", cfg.DBName, err)
	}

	// ----------------------------
	// Fluxo quando o DB JÁ EXISTE
	// ----------------------------
	if exists {
		// Modo automático (Ansible): NUNCA dropa; só aplica migrations
		if *auto {
			log.Printf("Banco de dados %q já existe. Modo --auto: não haverá drop. Apenas migrations serão aplicadas.\n", cfg.DBName)
			runAppMigrationsOrDie(cfg)
			return
		}

		// Modo manual, com --force: permitir drop/recreate com confirmação
		if *force {
			log.Printf("Banco de dados %q já existe e foi usado o parâmetro --force.", cfg.DBName)
			log.Printf("ATENÇÃO: isso irá APAGAR TODOS OS DADOS desse banco e recriá-lo do zero.")

			if !askYesNo(fmt.Sprintf("Tem certeza que deseja DROPAR e RECRIAR o banco %q? [s/N] ", cfg.DBName)) {
				log.Println("Operação cancelada pelo usuário. Nenhuma alteração foi feita.")
				return
			}

			log.Printf("Dropando banco %q...", cfg.DBName)
			if err := dropDatabase(adminDB, cfg.DBName); err != nil {
				log.Fatalf("erro dropando banco %q: %v", cfg.DBName, err)
			}
			log.Printf("Banco %q dropado com sucesso.", cfg.DBName)

			log.Printf("Criando banco %q novamente...", cfg.DBName)
			if err := createDatabase(adminDB, cfg.DBName); err != nil {
				log.Fatalf("erro recriando banco %q: %v", cfg.DBName, err)
			}
			log.Printf("Banco %q recriado com sucesso.\n", cfg.DBName)

			runAppMigrationsOrDie(cfg)
			return
		}

		// Modo manual, sem --force: só aplica migrations, não dropa
		log.Printf("Banco de dados %q já existe. Nenhum drop será feito. Aplicando migrations...\n", cfg.DBName)
		runAppMigrationsOrDie(cfg)
		return
	}

	// ----------------------------
	// Fluxo quando o DB NÃO EXISTE
	// ----------------------------

	// Modo automático (Ansible): cria sem perguntar
	if *auto {
		log.Printf("Banco de dados %q não existe. Modo --auto: criando banco automaticamente...", cfg.DBName)
		if err := createDatabase(adminDB, cfg.DBName); err != nil {
			log.Fatalf("erro criando banco %q: %v", cfg.DBName, err)
		}
		log.Printf("Banco %q criado com sucesso.\n", cfg.DBName)

		runAppMigrationsOrDie(cfg)
		return
	}

	// Modo manual: perguntar se deseja criar
	log.Printf("Banco de dados %q não existe.", cfg.DBName)
	if !askYesNo(fmt.Sprintf("Deseja criá-lo agora? [s/N] ")) {
		log.Println("Operação cancelada pelo usuário. Nenhuma alteração foi feita.")
		return
	}

	log.Printf("Criando banco %q...", cfg.DBName)
	if err := createDatabase(adminDB, cfg.DBName); err != nil {
		log.Fatalf("erro criando banco %q: %v", cfg.DBName, err)
	}
	log.Printf("Banco %q criado com sucesso.\n", cfg.DBName)

	runAppMigrationsOrDie(cfg)
}

// runAppMigrationsOrDie conecta no banco da aplicação e roda migrations.Run.
func runAppMigrationsOrDie(cfg *config.Config) {
	appDB, err := sql.Open("pgx", cfg.AppDSN())
	if err != nil {
		log.Fatalf("erro conectando ao banco da aplicação: %v", err)
	}
	defer appDB.Close()

	if err := appDB.Ping(); err != nil {
		log.Fatalf("erro no ping ao banco da aplicação: %v", err)
	}

	log.Println("Conectado ao banco da aplicação. Aplicando migrations...")

	if err := migrations.Run(appDB); err != nil {
		log.Fatalf("erro executando migrations: %v", err)
	}

	log.Println("Migrations aplicadas com sucesso. Banco pronto para uso.")
}

func databaseExists(db *sql.DB, name string) (bool, error) {
	var exists bool
	query := `SELECT EXISTS(SELECT 1 FROM pg_database WHERE datname = $1);`
	if err := db.QueryRow(query, name).Scan(&exists); err != nil {
		return false, err
	}
	return exists, nil
}

func createDatabase(db *sql.DB, name string) error {
	// Cria com UTF8 e template0 pra não herdar lixo estranho.
	stmt := fmt.Sprintf(
		`CREATE DATABASE "%s" WITH TEMPLATE=template0 ENCODING 'UTF8';`,
		name,
	)
	_, err := db.Exec(stmt)
	return err
}

func dropDatabase(db *sql.DB, name string) error {
	// encerra conexões ativas no banco alvo
	killStmt := `
SELECT pg_terminate_backend(pid)
FROM pg_stat_activity
WHERE datname = $1
  AND pid <> pg_backend_pid();
`
	if _, err := db.Exec(killStmt, name); err != nil {
		return fmt.Errorf("erro terminando conexões do banco %q: %w", name, err)
	}

	// DROP DATABASE não aceita parâmetro como identificador, então montamos com fmt.
	stmt := fmt.Sprintf(`DROP DATABASE "%s";`, name)
	if _, err := db.Exec(stmt); err != nil {
		return fmt.Errorf("erro executando DROP DATABASE %q: %w", name, err)
	}

	return nil
}

func askYesNo(prompt string) bool {
	fmt.Print(prompt)
	reader := bufio.NewReader(os.Stdin)
	line, _ := reader.ReadString('\n')
	line = strings.TrimSpace(strings.ToLower(line))
	return line == "s" || line == "sim" || line == "y" || line == "yes"
}
