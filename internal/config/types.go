package config

type Config struct {
	Platform PlatformConfig `yaml:"platform"`
	Apps     []AppConfig    `yaml:"apps"`
}

type PlatformConfig struct {
	DatabaseURL string `yaml:"database_url"`
}

type AppConfig struct {
	Name      string           `yaml:"name"`
	Auth      AuthConfig       `yaml:"auth"`
	Tables    []TableConfig    `yaml:"tables"`
	Storage   *StorageConfig   `yaml:"storage,omitempty" json:"storage,omitempty"`
	RateLimit *RateLimitConfig `yaml:"rate_limit,omitempty" json:"rate_limit,omitempty"`
}

type RateLimitConfig struct {
	Enabled           bool `json:"enabled" yaml:"enabled"`
	RequestsPerMinute int  `json:"requests_per_minute" yaml:"requests_per_minute"`
}

type StorageConfig struct {
	Bucket          string `json:"bucket" yaml:"bucket"`
	Region          string `json:"region" yaml:"region"`
	Endpoint        string `json:"endpoint" yaml:"endpoint"`
	AccessKeyID     string `json:"access_key_id" yaml:"access_key_id"`
	SecretAccessKey string `json:"secret_access_key,omitempty" yaml:"secret_access_key,omitempty"`
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
	Indexes []IndexConfig  `yaml:"indexes,omitempty"`
}

type ColumnConfig struct {
	Name       string           `json:"name" yaml:"name"`
	Type       string           `json:"type" yaml:"type"`
	Required   bool             `json:"required" yaml:"required"`
	Default    string           `json:"default" yaml:"default"`
	Unique     bool             `json:"unique" yaml:"unique"`
	RenameFrom string           `json:"rename_from,omitempty" yaml:"rename_from,omitempty"`
	References *ReferenceConfig `json:"references,omitempty" yaml:"references,omitempty"`
}

// ReferenceConfig declares a foreign key from this column to another table
// in the same app (cross-app references are out of scope — schema-per-app
// isolation is an existing architectural boundary).
type ReferenceConfig struct {
	Table  string `json:"table" yaml:"table"`
	Column string `json:"column" yaml:"column"`
	// OnDelete: cascade|restrict|set_null|no_action. Empty defaults to no_action.
	OnDelete string `json:"on_delete,omitempty" yaml:"on_delete,omitempty"`
}

type IndexConfig struct {
	Name    string   `json:"name" yaml:"name"`
	Columns []string `json:"columns" yaml:"columns"`
	Unique  bool     `json:"unique,omitempty" yaml:"unique,omitempty"`
}
