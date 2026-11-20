// Package config loads the environment shared by the server and the benchmark.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/pdd-tech/pdd-assistant/internal/llm"
)

// Search defaults are shared so a standard benchmark measures the server settings.
const (
	SearchK  = 5
	SearchK1 = 1.2
	SearchB  = 0.75
)

type Server struct {
	Addr          string
	DBPath        string
	CorpusDir     string
	AdminLogin    string
	AdminPassword string
	CookieSecure  bool
	LLM           llm.Config
}

func LoadServer() (Server, error) {
	login, password := strings.TrimSpace(os.Getenv("ADMIN_LOGIN")), os.Getenv("ADMIN_PASSWORD")
	if login == "" || strings.TrimSpace(password) == "" {
		return Server{}, fmt.Errorf("задайте ADMIN_LOGIN и ADMIN_PASSWORD")
	}
	secure, err := envBool("COOKIE_SECURE", false)
	if err != nil {
		return Server{}, err
	}
	model, err := LoadLLM()
	if err != nil {
		return Server{}, err
	}

	return Server{
		Addr:          envOr("ADDR", "127.0.0.1:0"),
		DBPath:        envOr("DB_PATH", "pdd-assistant.sqlite"),
		CorpusDir:     CorpusDir(),
		AdminLogin:    login,
		AdminPassword: password,
		CookieSecure:  secure,
		LLM:           model,
	}, nil
}

func LoadLLM() (llm.Config, error) {
	timeout, err := time.ParseDuration(envOr("LLM_TIMEOUT", "90s"))
	if err != nil || timeout <= 0 {
		return llm.Config{}, fmt.Errorf("LLM_TIMEOUT должен быть положительной длительностью, например 90s")
	}
	disableReasoning, err := envBool("LLM_DISABLE_REASONING", true)
	if err != nil {
		return llm.Config{}, err
	}

	return llm.Config{
		BaseURL:          envOr("LLM_BASE_URL", "http://127.0.0.1:11434/v1"),
		APIKey:           strings.TrimSpace(os.Getenv("LLM_API_KEY")),
		Model:            envOr("LLM_MODEL", "qwen3.5:9b"),
		Timeout:          timeout,
		DisableReasoning: disableReasoning,
	}, nil
}

func CorpusDir() string {
	return envOr("CORPUS_DIR", "corpus")
}

func envOr(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

func envBool(name string, fallback bool) (bool, error) {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback, nil
	}
	result, err := strconv.ParseBool(value)
	if err != nil {
		return false, fmt.Errorf("%s должен быть true или false", name)
	}
	return result, nil
}
