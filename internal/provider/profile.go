// Package provider owns all provider-specific facts.
package provider

import (
	"embed"
	"fmt"
	"go.yaml.in/yaml/v3"
)

//go:embed profile.yaml
var data embed.FS

type Pool struct {
	ID             string   `yaml:"id" json:"id"`
	Endpoint       string   `yaml:"endpoint" json:"endpoint"`
	Location       string   `yaml:"location" json:"location"`
	MultiAZ        bool     `yaml:"multi_az" json:"multi_az"`
	StorageClasses []string `yaml:"storage_classes" json:"storage_classes"`
}
type StorageClass struct {
	ID        string  `yaml:"id" json:"id"`
	NameRU    string  `yaml:"name_ru" json:"name_ru"`
	Price     float64 `yaml:"price_rub_gb_month" json:"price_rub_gb_month"`
	Verified  bool    `yaml:"verified" json:"verified"`
	CheckedAt string  `yaml:"checked_at" json:"checked_at"`
}
type Limits struct {
	MaxPutBytes int64          `yaml:"max_put_bytes" json:"max_put_bytes"`
	MaxParts    int32          `yaml:"max_parts" json:"max_parts"`
	RateLimits  map[string]int `yaml:"rate_limits" json:"rate_limits"`
}
type Profile struct {
	ID                string          `yaml:"id" json:"id"`
	DisplayName       string          `yaml:"display_name" json:"display_name"`
	EnvPrefix         string          `yaml:"env_prefix" json:"env_prefix"`
	URIScheme         string          `yaml:"uri_scheme" json:"uri_scheme"`
	Binary            string          `yaml:"binary" json:"binary"`
	DefaultPool       string          `yaml:"default_pool" json:"default_pool"`
	PathStyle         bool            `yaml:"path_style" json:"path_style"`
	Pools             []Pool          `yaml:"pools" json:"pools"`
	StorageClasses    []StorageClass  `yaml:"storage_classes" json:"storage_classes"`
	Features          map[string]bool `yaml:"features" json:"features"`
	Limits            Limits          `yaml:"limits" json:"limits"`
	PresignMaxSeconds int64           `yaml:"presign_max_seconds" json:"presign_max_seconds"`
	Verified          bool            `yaml:"verified" json:"verified"`
	PresignNote       string          `yaml:"presign_note" json:"presign_note"`
	DocsURLs          []string        `yaml:"docs_urls" json:"docs_urls"`
}

func Load() (Profile, error) {
	var p Profile
	b, e := data.ReadFile("profile.yaml")
	if e != nil {
		return p, e
	}
	if e = yaml.Unmarshal(b, &p); e != nil {
		return p, e
	}
	if p.Binary == "" || p.EnvPrefix == "" || len(p.Pools) == 0 {
		return p, fmt.Errorf("invalid provider profile")
	}
	return p, nil
}
