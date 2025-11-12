package config

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"

	"github.com/joho/godotenv"
)

type Config struct {
	DBHost    string
	DBPort    int
	DBUser    string
	DBPass    string
	DBName    string
	DBSSLMode string

	LogLevel       string
	WorkerPoolSize int
	RedisURL       string

	ProjectDir    string
	IncomingDir   string
	ProcessingDir string
	ProcessedDir  string
	FailedDir     string
	TmpDir        string
	IgnoredDir    string
}

// Load carrega variáveis de ambiente, tentando ler .env se existir.
func Load() (*Config, error) {
	// .env é opcional: se existir, carrega
	_ = godotenv.Load()

	getReq := func(key string) string {
		v := os.Getenv(key)
		if v == "" {
			log.Fatalf("variável de ambiente obrigatória ausente: %s", key)
		}
		return v
	}
	getOpt := func(key, def string) string {
		v := os.Getenv(key)
		if v == "" {
			return def
		}
		return v
	}

	// Banco
	host := getReq("NFE_DROP_DB_HOST")
	portStr := getReq("NFE_DROP_DB_PORT")
	user := getReq("NFE_DROP_DB_USER")
	pass := getOpt("NFE_DROP_DB_PASSWORD", "")
	name := getReq("NFE_DROP_DB_NAME")
	sslmode := getOpt("NFE_DROP_DB_SSLMODE", "disable")

	port, err := strconv.Atoi(portStr)
	if err != nil {
		return nil, fmt.Errorf("NFE_DROP_DB_PORT inválido: %w", err)
	}

	// App
	logLevel := getOpt("LOG_LEVEL", "info")
	workerPoolStr := getOpt("WORKER_POOL_SIZE", "5")
	workerPoolSize, err := strconv.Atoi(workerPoolStr)
	if err != nil {
		return nil, fmt.Errorf("WORKER_POOL_SIZE inválido: %w", err)
	}
	redisURL := getOpt("REDIS_URL", "redis://localhost:6379")

	// Diretório do projeto (base pros paths relativos)
	projectDir, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("não foi possível obter diretório de trabalho (pwd): %w", err)
	}
	projectDir, err = filepath.Abs(projectDir)
	if err != nil {
		return nil, fmt.Errorf("erro resolvendo diretório de trabalho: %w", err)
	}

	// Diretórios (podem ser relativos ou absolutos; se relativos, base = projectDir)
	incoming := resolveDir(projectDir, getOpt("INCOMING_DIR", "./incoming"))
	processing := resolveDir(projectDir, getOpt("PROCESSING_DIR", "./processing"))
	processed := resolveDir(projectDir, getOpt("PROCESSED_DIR", "./processed"))
	failed := resolveDir(projectDir, getOpt("FAILED_DIR", "./failed"))
	tmp := resolveDir(projectDir, getOpt("TMP_DIR", "./tmp"))
	ignored := resolveDir(projectDir, getOpt("IGNORED_DIR", "./ignored"))

	cfg := &Config{
		DBHost:    host,
		DBPort:    port,
		DBUser:    user,
		DBPass:    pass,
		DBName:    name,
		DBSSLMode: sslmode,

		LogLevel:       logLevel,
		WorkerPoolSize: workerPoolSize,
		RedisURL:       redisURL,

		ProjectDir:    projectDir,
		IncomingDir:   incoming,
		ProcessingDir: processing,
		ProcessedDir:  processed,
		FailedDir:     failed,
		TmpDir:        tmp,
		IgnoredDir:    ignored,
	}

	return cfg, nil
}

// resolveDir:
// - Se path for absoluto -> devolve como está.
// - Se path for relativo -> junta com baseDir.
func resolveDir(baseDir, path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(baseDir, path)
}

// DSN monta a string de conexão no formato "host=... port=... user=...".
func (c *Config) DSN(dbName string) string {
	base := fmt.Sprintf(
		"host=%s port=%d user=%s dbname=%s sslmode=%s",
		c.DBHost,
		c.DBPort,
		c.DBUser,
		dbName,
		c.DBSSLMode,
	)

	if c.DBPass != "" {
		base += fmt.Sprintf(" password=%s", c.DBPass)
	}

	return base
}

// AppDSN retorna o DSN para o banco da aplicação (NFE_DROP_DB_NAME).
func (c *Config) AppDSN() string {
	return c.DSN(c.DBName)
}

// AdminDSN retorna o DSN para o banco "postgres" (admin), usado para criar o DB da aplicação.
func (c *Config) AdminDSN() string {
	return c.DSN("postgres")
}
