package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	approvedModel                       = "gpt-5.4-mini-2026-03-17"
	openAIResponsesURL                  = "https://api.openai.com/v1/responses"
	maxAIInputTokens              int64 = 12000
	maxAIOutputTokens             int64 = 6000
	defaultCycleBudgetNanodollars int64 = 50000000 // $0.05.
	inputTokenNanodollars         int64 = 750      // $0.75 / million, uncached standard tier.
	outputTokenNanodollars        int64 = 4500     // $4.50 / million, including reasoning.
	maxAIResponseBytes                  = 1 << 20
	// All request text is UTF-8 byte-bounded, not estimated using characters/4.
	// Byte-level BPE cannot emit more ordinary tokens than source bytes. Counting
	// the entire JSON envelope overcounts escaped text and non-input settings.
	// Reserve a further 1024 tokens for the fixed two-message/schema framing.
	aiFramingTokenAllowance int64 = 1024
)

var (
	ErrAIBudget     = errors.New("AI request exceeds program budget or input limit")
	ErrAIAPI        = errors.New("OpenAI API failure")
	ErrAIRefusal    = errors.New("OpenAI refused the recommendation")
	ErrAIIncomplete = errors.New("OpenAI response is incomplete")
	ErrAIResponse   = errors.New("invalid OpenAI response")
)

type OpenAIPolicy struct {
	apiKey            string
	model             string
	budgetNanodollars int64
	client            *http.Client
	journal           RecommendationJournal
}

// NewOpenAIPolicy uses only the reviewed model/prices and the standard public
// endpoint. No environment endpoint, tool, retry, or fallback settings are read.
func NewOpenAIPolicy(apiKey, model string, budgetNanodollars int64, client *http.Client, journal RecommendationJournal) (*OpenAIPolicy, error) {
	if strings.TrimSpace(apiKey) == "" {
		return nil, fmt.Errorf("OPENAI_API_KEY is required for openai policy")
	}
	if model != approvedModel {
		return nil, fmt.Errorf("unsupported OpenAI model: reviewed pricing is required")
	}
	if budgetNanodollars <= 0 {
		return nil, fmt.Errorf("AI cycle budget must be positive")
	}
	if journal == nil {
		return nil, fmt.Errorf("recommendation journal is required")
	}
	if client == nil {
		client = &http.Client{}
	}
	copyClient := *client
	if copyClient.Timeout <= 0 || copyClient.Timeout > 60*time.Second {
		copyClient.Timeout = 60 * time.Second
	}
	// Never follow redirects that could create another generation request or
	// forward credentials away from the fixed endpoint.
	copyClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &OpenAIPolicy{apiKey: strings.TrimSpace(apiKey), model: model, budgetNanodollars: budgetNanodollars, client: &copyClient, journal: journal}, nil
}

func (p *OpenAIPolicy) requestBody(input RecommendationContext) ([]byte, int64, int64, error) {
	contextJSON, err := json.Marshal(input)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("encode AI context: %w", err)
	}
	request := map[string]any{
		"model": p.model, "store": false, "service_tier": "default", "truncation": "disabled",
		"max_output_tokens": maxAIOutputTokens, "reasoning": map[string]string{"effort": "low"},
		"tools": []any{}, "tool_choice": "none",
		"input": []map[string]string{
			{"role": "developer", "content": recommendationInstructions},
			{"role": "user", "content": string(contextJSON)},
		},
		"text": map[string]any{"format": map[string]any{"type": "json_schema", "name": "trading_recommendation", "strict": true, "schema": recommendationSchema()}},
	}
	body, err := json.Marshal(request)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("encode OpenAI request: %w", err)
	}
	upper, reserved, err := reserveAICost(int64(len(body)), p.budgetNanodollars)
	return body, upper, reserved, err
}

func reserveAICost(requestBytes, budget int64) (int64, int64, error) {
	// Bound before addition/multiplication so arbitrary inputs cannot overflow.
	if requestBytes < 0 || requestBytes > maxAIInputTokens-aiFramingTokenAllowance {
		return 0, 0, ErrAIBudget
	}
	upper := requestBytes + aiFramingTokenAllowance
	reserved := upper*inputTokenNanodollars + maxAIOutputTokens*outputTokenNanodollars
	if reserved > budget {
		return upper, reserved, ErrAIBudget
	}
	return upper, reserved, nil
}

func (p *OpenAIPolicy) Decide(ctx context.Context, snapshot DailySnapshot) (Decision, error) {
	if err := ctx.Err(); err != nil {
		return Decision{}, err
	}
	symbols := []string{snapshot.History.Symbol}
	for _, position := range snapshot.Positions {
		symbols = append(symbols, position.Symbol)
	}
	previous, err := p.journal.Latest(snapshot.Account.ID, symbols)
	if err != nil {
		return Decision{}, fmt.Errorf("load previous recommendations: %w", err)
	}
	input, err := buildRecommendationContext(snapshot, previous)
	if err != nil {
		return Decision{}, err
	}
	body, upper, reserved, err := p.requestBody(input)
	if err != nil {
		return Decision{}, err
	}
	id, err := newRecordID()
	if err != nil {
		return Decision{}, err
	}
	record := RecommendationRecord{
		Version: 1, ID: id, CreatedAt: time.Now().UTC(), AccountID: snapshot.Account.ID,
		Symbol: input.Symbol, Model: p.model, PromptVersion: promptVersion, Input: input,
		Status: "reserved", Validation: executionDisabled("pending", snapshot),
		BudgetNanodollars: p.budgetNanodollars, ReservedNanodollars: reserved, InputTokenUpperBound: upper,
	}
	// Persist the reservation first. If a process dies or a timeout occurs, this
	// remains potentially billed; it is never automatically retried or refunded.
	if err := p.journal.Save(record); err != nil {
		return Decision{}, fmt.Errorf("reserve recommendation journal: %w", err)
	}
	d, usage, err := p.generate(ctx, body, upper)
	record.Usage = usage
	if err != nil {
		record.Status = "failed"
		record.Failure = err.Error()
		record.Validation = executionDisabled("failed", snapshot)
	} else {
		record.Recommendation = &d
		if err = validateRecommendation(d, input); err != nil {
			record.Status = "invalid"
			record.Failure = err.Error()
			record.Validation = executionDisabled("invalid", snapshot)
		} else {
			record.Status = "validated"
			record.Validation = executionDisabled("valid", snapshot)
		}
	}
	if saveErr := p.journal.Save(record); saveErr != nil {
		return Decision{}, errors.Join(err, fmt.Errorf("save recommendation journal: %w", saveErr))
	}
	if err != nil {
		return Decision{}, err
	}
	d.RecordID, d.Usage = id, usage
	return d, nil
}

type openAIResponse struct {
	Status      string `json:"status"`
	Model       string `json:"model"`
	ServiceTier string `json:"service_tier"`
	Usage       *struct {
		InputTokens  *int64 `json:"input_tokens"`
		OutputTokens *int64 `json:"output_tokens"`
	} `json:"usage"`
	Output []struct {
		Type    string `json:"type"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	} `json:"output"`
}

func (p *OpenAIPolicy) generate(ctx context.Context, body []byte, upper int64) (Decision, *TokenUsage, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, openAIResponsesURL, bytes.NewReader(body))
	if err != nil {
		return Decision{}, nil, fmt.Errorf("build OpenAI request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+p.apiKey)
	req.Header.Set("Content-Type", "application/json")
	response, err := p.client.Do(req) // Exactly one attempt; no SDK retries.
	if err != nil {
		// Do not expose transport error text that might echo credentials or data.
		if ctx.Err() != nil {
			return Decision{}, nil, fmt.Errorf("OpenAI request canceled: %w", ctx.Err())
		}
		var timeout interface{ Timeout() bool }
		if errors.As(err, &timeout) && timeout.Timeout() {
			return Decision{}, nil, fmt.Errorf("OpenAI request timed out: %w", context.DeadlineExceeded)
		}
		return Decision{}, nil, ErrAIAPI
	}
	data, readErr := io.ReadAll(io.LimitReader(response.Body, maxAIResponseBytes+1))
	closeErr := response.Body.Close()
	if ctx.Err() != nil {
		return Decision{}, nil, fmt.Errorf("OpenAI response canceled: %w", ctx.Err())
	}
	if readErr != nil || closeErr != nil {
		var timeout interface{ Timeout() bool }
		if errors.As(errors.Join(readErr, closeErr), &timeout) && timeout.Timeout() {
			return Decision{}, nil, fmt.Errorf("OpenAI response timed out: %w", context.DeadlineExceeded)
		}
		return Decision{}, nil, fmt.Errorf("read or close OpenAI response: %w", ErrAIResponse)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return Decision{}, nil, fmt.Errorf("%w (HTTP %d)", ErrAIAPI, response.StatusCode)
	}
	if len(data) > maxAIResponseBytes {
		return Decision{}, nil, fmt.Errorf("OpenAI response exceeds size limit: %w", ErrAIResponse)
	}
	var result openAIResponse
	if err := json.Unmarshal(data, &result); err != nil {
		return Decision{}, nil, ErrAIResponse
	}
	var usage *TokenUsage
	if result.Usage != nil && result.Usage.InputTokens != nil && result.Usage.OutputTokens != nil {
		i, o := *result.Usage.InputTokens, *result.Usage.OutputTokens
		if i < 0 || o < 0 || i > maxAIInputTokens || o > maxAIOutputTokens {
			return Decision{}, nil, fmt.Errorf("OpenAI usage exceeds reviewed limits: %w", ErrAIResponse)
		}
		usage = &TokenUsage{InputTokens: i, OutputTokens: o, CostUpperNanodollars: i*inputTokenNanodollars + o*outputTokenNanodollars}
		if i > upper {
			return Decision{}, usage, fmt.Errorf("OpenAI input usage exceeds reservation: %w", ErrAIResponse)
		}
	}
	if result.Model != p.model || result.ServiceTier != "default" {
		return Decision{}, usage, fmt.Errorf("unreviewed OpenAI model or service tier: %w", ErrAIResponse)
	}
	if result.Status == "incomplete" {
		return Decision{}, usage, ErrAIIncomplete
	}
	if result.Status != "completed" {
		return Decision{}, usage, ErrAIResponse
	}
	var texts []string
	for _, item := range result.Output {
		if item.Type == "reasoning" {
			continue
		}
		if item.Type != "message" {
			return Decision{}, usage, ErrAIResponse
		}
		for _, content := range item.Content {
			switch content.Type {
			case "refusal":
				return Decision{}, usage, ErrAIRefusal
			case "output_text":
				texts = append(texts, content.Text)
			default:
				return Decision{}, usage, ErrAIResponse
			}
		}
	}
	if len(texts) != 1 || usage == nil {
		return Decision{}, usage, ErrAIResponse
	}
	d, err := decodeRecommendation([]byte(texts[0]))
	return d, usage, err
}
