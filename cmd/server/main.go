// Command server serves the local driving-school assistant.
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/pdd-tech/pdd-assistant/internal/auth"
	"github.com/pdd-tech/pdd-assistant/internal/config"
	"github.com/pdd-tech/pdd-assistant/internal/llm"
	"github.com/pdd-tech/pdd-assistant/internal/rag"
	"github.com/pdd-tech/pdd-assistant/internal/search"
	"github.com/pdd-tech/pdd-assistant/internal/store"
	"github.com/pdd-tech/pdd-assistant/internal/ui"
	"github.com/pdd-tech/pdd-assistant/internal/web"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := run(ctx, log.Default()); err != nil {
		log.Printf("ошибка запуска/работы сервера: %v", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, logger *log.Logger) error {
	cfg, err := config.LoadServer()
	if err != nil {
		return err
	}
	client, err := llm.New(cfg.LLM)
	if err != nil {
		return fmt.Errorf("настройки LLM: %w", err)
	}
	docs, err := search.LoadCorpus(cfg.CorpusDir)
	if err != nil {
		return fmt.Errorf("загрузка корпуса: %w", err)
	}
	index, err := search.New(docs, config.SearchK1, config.SearchB)
	if err != nil {
		return fmt.Errorf("построение BM25: %w", err)
	}
	ragService, err := rag.New(index, config.SearchK, len(docs), client)
	if err != nil {
		return fmt.Errorf("настройки RAG: %w", err)
	}
	renderer, err := ui.New()
	if err != nil {
		return fmt.Errorf("загрузка шаблонов: %w", err)
	}
	db, err := store.Open(cfg.DBPath)
	if err != nil {
		return fmt.Errorf("открытие SQLite: %w", err)
	}
	defer db.Close()
	if err := db.EnsureAdmin(ctx, cfg.AdminLogin, cfg.AdminPassword); err != nil {
		return fmt.Errorf("инициализация администратора: %w", err)
	}

	server := &http.Server{
		Handler:           web.New(renderer, db, auth.New(cfg.CookieSecure), ragService, logger),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		IdleTimeout:       60 * time.Second,
		ErrorLog:          logger,
	}
	listener, err := net.Listen("tcp", cfg.Addr)
	if err != nil {
		return fmt.Errorf("прослушивание ADDR: %w", err)
	}
	logger.Printf("pdd-assistant: http://%s", listener.Addr())

	errs := make(chan error, 1)
	go func() {
		errs <- server.Serve(listener)
	}()
	select {
	case err := <-errs:
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			_ = server.Close()
			return err
		}
		if err := <-errs; !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	}
}
