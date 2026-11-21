package llm

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestCheckOpenRouterRejectsUnavailableOrPaidModelAndExhaustedQuota(t *testing.T) {
	for _, tt := range []struct {
		name, catalog, key string
		rejected           bool
	}{
		{"free", `{"data":[{"id":"test/model:free","pricing":{"prompt":"0","completion":"0"}}]}`, `{"data":{"free_model_daily_requests":{"remaining":40,"limit":50}}}`, false},
		{"removed", `{"data":[]}`, `{"data":{}}`, true},
		{"nonzero price", `{"data":[{"id":"test/model:free","pricing":{"prompt":"0.01","completion":"0"}}]}`, `{"data":{}}`, true},
		{"unknown price", `{"data":[{"id":"test/model:free","pricing":{}}]}`, `{"data":{}}`, true},
		{"daily exhausted", `{"data":[{"id":"test/model:free","pricing":{"prompt":"0","completion":"0"}}]}`, `{"data":{"free_model_daily_requests":{"remaining":0,"limit":50}}}`, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			c, err := New(Config{BaseURL: "https://openrouter.ai/api/v1", Model: "test/model:free", APIKey: "private-key", Timeout: time.Second})
			if err != nil {
				t.Fatal(err)
			}
			c.httpClient.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
				body := tt.catalog
				if strings.HasSuffix(r.URL.Path, "/key") {
					if r.Header.Get("Authorization") != "Bearer private-key" {
						t.Error("key check has no authorization")
					}
					body = tt.key
				} else if r.Header.Get("Authorization") != "" {
					t.Error("key unnecessarily sent to public catalog")
				}
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
			})
			_, err = c.CheckOpenRouter(context.Background())
			if (err != nil) != tt.rejected {
				t.Fatalf("err=%v rejected=%v", err, tt.rejected)
			}
			if err != nil && strings.Contains(err.Error(), "private-key") {
				t.Error("key leaked in error")
			}
		})
	}
}

func TestOpenRouterDisablesReasoningWithSupportedParameter(t *testing.T) {
	c, err := New(Config{BaseURL: "https://openrouter.ai/api/v1", Model: "test/model:free", APIKey: "private-key", Timeout: time.Second, DisableReasoning: true})
	if err != nil {
		t.Fatal(err)
	}
	c.httpClient.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), `"reasoning":{"enabled":false}`) || strings.Contains(string(body), `"reasoning_effort"`) {
			t.Error("unsupported reasoning parameter")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"choices":[{"finish_reason":"stop","message":{"content":"{\"refused\":true,\"parts\":[],\"missing\":\"срок\"}"}}]}`)), Header: make(http.Header)}, nil
	})
	got, err := c.Complete(context.Background(), "Какой срок?", nil)
	if err != nil || !got.Refused {
		t.Fatalf("got=%+v err=%v", got, err)
	}
}
