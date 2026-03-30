package llm

type Config struct {
	Endpoints Endpoints `yaml:"endpoints"`
}

type Endpoint struct {
	Provider  Provider `yaml:"provider"`
	APIURL    string   `yaml:"api_url,omitempty"`
	APIKey    string   `yaml:"api_key,omitempty"`
	ModelName string   `yaml:"model_name,omitempty"`
}

type Provider string

const (
	UndefinedProvider = Provider("")
	ProviderChatGPT   = Provider("ChatGPT")
)

type Endpoints map[string]*Endpoint

func (s Endpoints) FirstByProvider(p Provider) *Endpoint {
	for _, b := range s {
		if b.Provider == p {
			return b
		}
	}
	return nil
}

func (s Endpoints) GetAPIKey(
	name string,
) string {
	b := s[name]
	if b == nil {
		return ""
	}
	return b.APIKey
}
