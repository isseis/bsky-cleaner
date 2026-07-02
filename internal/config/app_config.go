package config

// AppConfig combines the non-secret configuration values and the secret
// credentials into the single value callers need to run the tool.
type AppConfig struct {
	Config
	Credentials
}

// LoadAppConfig combines Load and LoadCredentials into a single entry
// point for callers that need both. If either call fails, its error is
// returned unchanged.
func LoadAppConfig(path string) (*AppConfig, error) {
	cfg, err := Load(path)
	if err != nil {
		return nil, err
	}

	creds, err := LoadCredentials()
	if err != nil {
		return nil, err
	}

	return &AppConfig{Config: *cfg, Credentials: *creds}, nil
}
