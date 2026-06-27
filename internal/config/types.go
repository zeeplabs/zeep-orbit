package config

type Config struct {
	Platform PlatformConfig `yaml:"platform"`
	Apps     []AppConfig    `yaml:"apps"`
}

type PlatformConfig struct {
	DatabaseURL string `yaml:"database_url"`
}

type AppConfig struct {
	Name   string        `yaml:"name"`
	Auth   AuthConfig    `yaml:"auth"`
	Tables []TableConfig `yaml:"tables"`
}

type AuthConfig struct {
	JWTSecret string        `yaml:"jwt_secret"`
	Providers AuthProviders `yaml:"providers"`
}

type AuthProviders struct {
	Email bool `yaml:"email"`
}

type TableConfig struct {
	Name    string         `yaml:"name"`
	RLS     string         `yaml:"rls"`
	Columns []ColumnConfig `yaml:"columns"`
}

type ColumnConfig struct {
	Name     string `yaml:"name"`
	Type     string `yaml:"type"`
	Required bool   `yaml:"required"`
	Default  string `yaml:"default"`
	Unique   bool   `yaml:"unique"`
}
