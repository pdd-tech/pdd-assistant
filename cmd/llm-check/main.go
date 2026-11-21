// Command llm-check verifies the free API profile before a runtime benchmark.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/pdd-tech/pdd-assistant/internal/config"
	"github.com/pdd-tech/pdd-assistant/internal/llm"
	"github.com/pdd-tech/pdd-assistant/internal/search"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := run(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "Проверка API:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	cfg, err := config.LoadLLM()
	if err != nil {
		return err
	}
	client, err := llm.New(cfg)
	if err != nil {
		return err
	}
	availability, err := client.CheckOpenRouter(ctx)
	if err != nil {
		return err
	}
	fmt.Println("Модель доступна в каталоге, цена входных и выходных токенов — 0:", cfg.Model)
	if availability.DailyRemaining != nil {
		fmt.Printf("Остаток дневной квоты перед проверкой генерации: %d", *availability.DailyRemaining)
		if availability.DailyLimit != nil {
			fmt.Printf(" из %d", *availability.DailyLimit)
		}
		fmt.Println()
	}
	// Synthetic facts exercise grounding and numerical conditions without benchmark leakage.
	answer, err := client.Complete(ctx, "На вымышленной площадке я задержался на 13 минут. Какой сбор и куда сдавать пропуск?", []search.Document{
		{ID: "demo-fee", Ref: "Учебное правило А", Text: "При задержке не более 10 минут сбор составляет 15 условных единиц. При задержке свыше 10 минут сбор составляет 25 условных единиц."},
		{ID: "demo-return", Ref: "Учебное правило Б", Text: "Пропуск сдают дежурному на выезде."},
	})
	if err != nil {
		return err
	}
	fmt.Println("Реальный ответ API на синтетический пример:")
	fmt.Println(answer.Answer)
	if answer.Refused {
		return fmt.Errorf("модель отказала на примере с достаточными данными; нужна проверка качества")
	}
	fmt.Println("Генерация завершена. Смысл ответа нужно проверить отдельно; перед benchmark повторно проверьте квоту.")
	return nil
}
