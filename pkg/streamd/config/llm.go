package config

type LLM struct {
	Endpoints LLMEndpoints `yaml:"endpoints"`
}

type LLMEndpoint struct {
	Provider  LLMProvider `yaml:"provider"`
	APIURL    string      `yaml:"api_url,omitempty"`
	APIKey    string      `yaml:"api_key,omitempty"` // TODO: this should be secret.String
	ModelName string      `yaml:"model_name,omitempty"`
}

type LLMProvider string

const (
	UndefinedLLMProvider = LLMProvider("")
	LLMProviderChatGPT   = LLMProvider("ChatGPT")
)

type LLMEndpoints map[string]*LLMEndpoint

func (s LLMEndpoints) FirstByProvider(p LLMProvider) *LLMEndpoint {
	for _, b := range s {
		if b.Provider == p {
			return b
		}
	}
	return nil
}

func (s LLMEndpoints) GetAPIKey(
	name string,
) string {
	b := s[name]
	if b == nil {
		return ""
	}
	return b.APIKey
}
