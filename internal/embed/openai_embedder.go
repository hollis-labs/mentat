package embed

import (
	"context"
	"errors"
	"net/http"

	embedcontracts "github.com/hollis-labs/substrate/llm-core/embedcontracts"
	sdk "github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
)

type OpenAIEmbedder struct {
	sdk sdk.Client
}

var _ embedcontracts.Embedder = (*OpenAIEmbedder)(nil)

func NewOpenAIEmbedder(apiKey string, httpClient *http.Client, opts ...option.RequestOption) *OpenAIEmbedder {
	base := []option.RequestOption{option.WithMaxRetries(0)}
	if apiKey != "" {
		base = append(base, option.WithAPIKey(apiKey))
	}
	if httpClient != nil {
		base = append(base, option.WithHTTPClient(httpClient))
	}
	base = append(base, opts...)

	return &OpenAIEmbedder{sdk: sdk.NewClient(base...)}
}

func (e *OpenAIEmbedder) Embed(ctx context.Context, text, model string) (*embedcontracts.EmbeddingResult, error) {
	if model == "" {
		return nil, errors.New("openai embed: model is required")
	}
	resp, err := e.sdk.Embeddings.New(ctx, sdk.EmbeddingNewParams{
		Input: sdk.EmbeddingNewParamsInputUnion{OfString: sdk.String(text)},
		Model: sdk.EmbeddingModel(model),
	})
	if err != nil {
		return nil, err
	}
	if len(resp.Data) == 0 {
		return nil, errors.New("openai embed: empty response")
	}
	return &embedcontracts.EmbeddingResult{
		Embedding:  toFloat32(resp.Data[0].Embedding),
		TokenCount: int(resp.Usage.TotalTokens),
	}, nil
}

func (e *OpenAIEmbedder) EmbedBatch(ctx context.Context, texts []string, model string) ([]embedcontracts.EmbeddingResult, error) {
	if model == "" {
		return nil, errors.New("openai embed-batch: model is required")
	}
	if len(texts) == 0 {
		return nil, nil
	}
	resp, err := e.sdk.Embeddings.New(ctx, sdk.EmbeddingNewParams{
		Input: sdk.EmbeddingNewParamsInputUnion{OfArrayOfStrings: texts},
		Model: sdk.EmbeddingModel(model),
	})
	if err != nil {
		return nil, err
	}
	if len(resp.Data) == 0 {
		return nil, errors.New("openai embed-batch: empty response")
	}
	if len(resp.Data) != len(texts) {
		return nil, errors.New("openai embed-batch: response count mismatch")
	}
	out := make([]embedcontracts.EmbeddingResult, len(resp.Data))
	for i, item := range resp.Data {
		out[i] = embedcontracts.EmbeddingResult{
			Embedding:  toFloat32(item.Embedding),
			TokenCount: 0,
		}
	}
	return out, nil
}

func (e *OpenAIEmbedder) EmbeddingDimensions(model string) int {
	switch model {
	case "text-embedding-3-small":
		return 1536
	case "text-embedding-3-large":
		return 3072
	case "text-embedding-ada-002":
		return 1536
	default:
		return 0
	}
}

func toFloat32(vec []float64) []float32 {
	out := make([]float32, len(vec))
	for i, v := range vec {
		out[i] = float32(v)
	}
	return out
}
