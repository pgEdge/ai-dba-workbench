/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package llm

import (
	"context"
	"errors"
	"fmt"
	"math"

	pgllm "github.com/pgEdge/pgedge-go-llm-lib/llm"
	"github.com/pgedge/ai-workbench/alerter/internal/config"
)

// DecisionProvider classifies a Tier 3 anomaly candidate with a decision
// model, which returns the probability of a yes/no answer rather than
// generated text (GitHub issue #626).
type DecisionProvider interface {
	// ClassifyDecision asks the decision model whether the anomaly
	// described by input is a real issue that requires attention, and
	// applies suppressThreshold to the answer: the verdict suppresses
	// the candidate when the probability that it is not a real issue is
	// at least suppressThreshold. A refusal, a missing answer or a
	// probability outside [0, 1] is an error, never a verdict.
	ClassifyDecision(ctx context.Context, input string, suppressThreshold float64) (DecisionVerdict, error)

	// Ping checks that the provider is reachable and accepts the API
	// key, without spending any tokens.
	Ping(ctx context.Context) error

	// ModelName returns the name of the decision model being used.
	ModelName() string

	// ProviderName returns the name of the provider that serves the
	// decision model, such as "typesafe" or "openai".
	ProviderName() string
}

// DecisionVerdict is the outcome of DecisionProvider.ClassifyDecision.
type DecisionVerdict struct {
	// Alert is true when the candidate should be raised as an alert,
	// and false when it should be suppressed.
	Alert bool

	// RealIssueProbability is the model's probability that the anomaly
	// is a real issue that requires attention.
	RealIssueProbability float64
}

// decisionQuestionName names the single yes/no question in every
// decision request, and the answer read back from the response.
const decisionQuestionName = "real"

// decisionQuestion is the question put to the decision model. Its text
// is the one measured in the issue #626 spike; the context it is asked
// about comes from the engine's classification prompt, without that
// prompt's JSON response instructions.
const decisionQuestion = "Is this a real issue that requires attention?"

// ErrDecisionRefused reports that the decision model declined to answer.
var ErrDecisionRefused = errors.New("decision model refused to answer")

// defaultDecisionModels maps a decision provider to the model used when
// the configuration does not name one.
var defaultDecisionModels = map[string]string{
	"typesafe": "jev-latest",
	"openai":   "gpt-6-luna",
}

// libDecision implements DecisionProvider by delegating to the
// pgedge-go-llm-lib Client.Decide method.
type libDecision struct {
	client   pgllm.Client
	provider string
	model    string
}

// ClassifyDecision implements DecisionProvider.
func (d *libDecision) ClassifyDecision(ctx context.Context, input string, suppressThreshold float64) (DecisionVerdict, error) {
	resp, err := d.client.Decide(ctx, pgllm.DecisionRequest{
		// Model is always set: the OpenAI provider ignores
		// Options.Model for decisions and would otherwise use its own
		// default.
		Model: d.model,
		Input: input,
		Questions: []pgllm.DecisionQuestion{{
			Name:         decisionQuestionName,
			Kind:         pgllm.DecisionYesNo,
			Instructions: decisionQuestion,
		}},
	})
	if err != nil {
		return DecisionVerdict{}, err
	}

	answer, ok := resp.Answer(decisionQuestionName)
	if !ok {
		return DecisionVerdict{}, fmt.Errorf("%w: no answer to the decision question", ErrInvalidResponse)
	}
	if answer.Refused {
		return DecisionVerdict{}, ErrDecisionRefused
	}
	p := answer.Probability
	if math.IsNaN(p) || p < 0 || p > 1 {
		return DecisionVerdict{}, fmt.Errorf("%w: decision probability %v is outside [0, 1]", ErrInvalidResponse, p)
	}

	return DecisionVerdict{
		Alert:                !DecisionSuppresses(p, suppressThreshold),
		RealIssueProbability: p,
	}, nil
}

// DecisionSuppresses reports whether a candidate whose probability of
// being a real issue is realIssueProbability is suppressed under
// suppressThreshold: that is, whether the probability that it is not a
// real issue is at least the threshold.
func DecisionSuppresses(realIssueProbability, suppressThreshold float64) bool {
	return 1-realIssueProbability >= suppressThreshold
}

// Ping implements DecisionProvider.
func (d *libDecision) Ping(ctx context.Context) error {
	return d.client.Ping(ctx)
}

// ModelName implements DecisionProvider. It returns the configured model
// rather than the client's, since that is the model every request names.
func (d *libDecision) ModelName() string {
	return d.model
}

// ProviderName implements DecisionProvider.
func (d *libDecision) ProviderName() string {
	return d.provider
}

// newLibDecision builds a libDecision for the named provider, falling back
// to the provider's default model when model is empty.
func newLibDecision(provider, apiKey, model, baseURL string) (DecisionProvider, error) {
	if model == "" {
		model = defaultDecisionModels[provider]
	}
	client, err := pgllm.NewClient(provider, pgllm.Options{
		APIKey:  apiKey,
		Model:   model,
		BaseURL: baseURL,
	})
	if err != nil {
		return nil, fmt.Errorf("create %s decision client: %w", provider, err)
	}
	return &libDecision{client: client, provider: provider, model: model}, nil
}

// NewDecisionProvider creates the decision provider selected by
// llm.decision_provider. It returns nil and no error when none is
// selected.
//
// The TypeSafe API key comes from llm.typesafe.api_key_file only, like
// every other provider's key; the library's TYPESAFE_API_KEY fallback is
// never reached because a missing key is rejected here first. OpenAI
// follows the reasoning provider's rule, accepting a base URL without a
// key for a local, keyless endpoint.
func NewDecisionProvider(cfg *config.Config) (DecisionProvider, error) {
	if cfg == nil {
		return nil, nil
	}

	switch cfg.LLM.DecisionProvider {
	case "typesafe":
		apiKey := cfg.GetTypeSafeAPIKey()
		if apiKey == "" {
			return nil, fmt.Errorf("typesafe: %w", ErrAPIKeyMissing)
		}
		return newLibDecision("typesafe", apiKey, cfg.LLM.TypeSafe.DecisionModel, cfg.LLM.TypeSafe.BaseURL)

	case "openai":
		apiKey := cfg.GetOpenAIAPIKey()
		if apiKey == "" && cfg.LLM.OpenAI.BaseURL == "" {
			return nil, fmt.Errorf("openai: %w", ErrAPIKeyMissing)
		}
		return newLibDecision("openai", apiKey, cfg.LLM.OpenAI.DecisionModel, cfg.LLM.OpenAI.BaseURL)

	case "", "none", "disabled":
		return nil, nil

	default:
		return nil, fmt.Errorf("unknown decision provider: %s", cfg.LLM.DecisionProvider)
	}
}
