package vali

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

// LoadCatalogs reads one Catalog per TOML or JSON file in dir, using each
// file's basename without its extension as the locale key. Pass the result to
// Config.Catalogs. Other file types and subdirectories are skipped.
// Unknown catalog keys are reported as errors.
func LoadCatalogs(dir string) (map[string]Catalog, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("load catalogs: %w", err)
	}
	catalogs := make(map[string]Catalog)
	seen := make(map[string]string)
	for _, entry := range entries {
		name := entry.Name()
		extension := filepath.Ext(name)
		format := strings.ToLower(extension)
		if entry.IsDir() || (format != ".toml" && format != ".json") {
			continue
		}
		path := filepath.Join(dir, name)
		locale := strings.TrimSuffix(name, extension)
		normalized := normalizeLocale(locale)
		if !localeSyntax.MatchString(normalized) {
			return nil, fmt.Errorf("load catalog %q: invalid locale %q", path, locale)
		}
		if previous, ok := seen[normalized]; ok {
			return nil, fmt.Errorf("load catalog %q: locale %q is also defined in %q", path, normalized, previous)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("load catalog: %w", err)
		}
		catalog, err := decodeCatalog(data, format)
		if err != nil {
			return nil, fmt.Errorf("decode catalog %q: %w", path, err)
		}
		seen[normalized] = path
		catalogs[locale] = catalog
	}
	return catalogs, nil
}

func decodeCatalog(data []byte, format string) (Catalog, error) {
	if format == ".toml" {
		var catalog Catalog
		metadata, err := toml.NewDecoder(bytes.NewReader(data)).Decode(&catalog)
		if err != nil {
			return Catalog{}, err
		}
		if unknown := metadata.Undecoded(); len(unknown) > 0 {
			return Catalog{}, fmt.Errorf("unknown catalog key %q", unknown[0].String())
		}
		return catalog, nil
	}

	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var catalog *Catalog
	if err := decoder.Decode(&catalog); err != nil {
		return Catalog{}, err
	}
	if catalog == nil {
		return Catalog{}, fmt.Errorf("expected a JSON object")
	}
	if err := decoder.Decode(new(json.RawMessage)); err != io.EOF {
		return Catalog{}, fmt.Errorf("expected exactly one JSON object")
	}
	return *catalog, nil
}
