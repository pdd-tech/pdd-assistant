package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

type Availability struct {
	DailyRemaining *int
	DailyLimit     *int
}

// Check validates model access without making a generation request.
func (c *Client) Check(ctx context.Context) (Availability, error) {
	u, _ := url.Parse(c.settings.BaseURL)
	if u.Hostname() == "openrouter.ai" {
		return c.CheckOpenRouter(ctx)
	}
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	var catalog struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := c.readMetadata(ctx, "/models", true, &catalog); err != nil {
		return Availability{}, err
	}
	for _, m := range catalog.Data {
		if strings.TrimPrefix(m.ID, "models/") == c.settings.Model {
			return Availability{}, nil
		}
	}
	return Availability{}, errors.New("выбранная модель недоступна этому ключу; проверьте LLM_MODEL и доступ проекта")
}

// CheckOpenRouter checks metadata without spending a generation request.
func (c *Client) CheckOpenRouter(ctx context.Context) (Availability, error) {
	u, _ := url.Parse(c.settings.BaseURL)
	if u == nil || u.Hostname() != "openrouter.ai" {
		return Availability{}, errors.New("проверка каталога доступна для профиля OpenRouter")
	}
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	var catalog struct {
		Data []struct {
			ID      string `json:"id"`
			Pricing struct {
				Prompt     string `json:"prompt"`
				Completion string `json:"completion"`
			} `json:"pricing"`
		} `json:"data"`
	}
	if err := c.readMetadata(ctx, "/models", false, &catalog); err != nil {
		return Availability{}, err
	}
	found := false
	for _, model := range catalog.Data {
		if model.ID != c.settings.Model {
			continue
		}
		prompt, err1 := strconv.ParseFloat(model.Pricing.Prompt, 64)
		completion, err2 := strconv.ParseFloat(model.Pricing.Completion, 64)
		if err1 != nil || err2 != nil || prompt != 0 || completion != 0 {
			return Availability{}, errors.New("каталог не подтверждает нулевую цену выбранной модели")
		}
		found = true
		break
	}
	if !found {
		return Availability{}, errors.New("выбранная модель отсутствует в каталоге OpenRouter; проверьте LLM_MODEL")
	}
	var key struct {
		Data struct {
			FreeRequests struct {
				Remaining *int `json:"remaining"`
				Limit     *int `json:"limit"`
			} `json:"free_model_daily_requests"`
		} `json:"data"`
	}
	if err := c.readMetadata(ctx, "/key", true, &key); err != nil {
		return Availability{}, err
	}
	availability := Availability{DailyRemaining: key.Data.FreeRequests.Remaining, DailyLimit: key.Data.FreeRequests.Limit}
	if availability.DailyRemaining != nil && *availability.DailyRemaining <= 0 {
		return availability, errors.New("дневная квота бесплатных моделей исчерпана")
	}
	return availability, nil
}

func (c *Client) readMetadata(ctx context.Context, path string, authenticated bool, target any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.settings.BaseURL+path, nil)
	if err != nil {
		return errors.New("LLM: не удалось создать запрос метаданных")
	}
	if authenticated {
		token, err := c.bearer(ctx)
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return errors.New("LLM: API метаданных недоступен")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("LLM: API метаданных вернул HTTP %d", resp.StatusCode)
	}
	const limit = 8 << 20
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil || len(data) > limit {
		return errors.New("LLM: не удалось прочитать метаданные")
	}
	if err := json.Unmarshal(data, target); err != nil {
		return errors.New("LLM: некорректные метаданные API")
	}
	return nil
}
