package vali

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadCatalogs(t *testing.T) {
	dir := t.TempDir()
	writeCatalogFile(t, dir, "zh-CN.TOML", `
		[labels]
		"form.email" = "邮箱"

		[messages]
		required = "请输入 :attribute。"

		[field_messages.email]
		email = "请检查 :attribute。"
	`)
	writeCatalogFile(t, dir, "en.JSON", `{"labels":{"form.email":"Email"}}`)
	writeCatalogFile(t, dir, "notes.txt", "ignored")
	if err := os.Mkdir(filepath.Join(dir, "backup.toml"), 0o755); err != nil {
		t.Fatal(err)
	}
	catalogs, err := LoadCatalogs(dir)
	if err != nil || len(catalogs) != 2 || catalogs["zh-CN"].Labels["form.email"] != "邮箱" {
		t.Fatalf("got %v, %v", catalogs, err)
	}
	v := newTestValidator(t, Config{Catalogs: catalogs})
	type form struct {
		Email string `json:"email" label:"form.email" validate:"bail|required|email"`
	}
	for _, tc := range []struct{ locale, email, want string }{
		{"zh-CN", "", "请输入 邮箱。"},
		{"zh-Hans", "broken", "请检查 邮箱。"},
		{"en-US", "", "Email is required."},
	} {
		fields, err := v.ValidateStruct(context.Background(), form{Email: tc.email}, StructOptions{Locale: tc.locale})
		if err != nil || len(fields) != 1 || fields[0].Message != tc.want {
			t.Errorf("%s got %v, %v; want %q", tc.locale, fields, err, tc.want)
		}
	}
}

func TestLoadCatalogsErrors(t *testing.T) {
	for _, tc := range []struct {
		name, data, want string
	}{
		{"fr.json", "{", "fr.json"},
		{"ja.json", "null", "ja.json"},
		{"fr.json", "{\"field_mesages\":{}}", "fr.json\": json: unknown field \"field_mesages\""},
		{"fr.json", "{} {}", "expected exactly one JSON object"},
		{"ja.toml", "[field_mesages]\n", "ja.toml\": unknown catalog key \"field_mesages\""},
		{"not a locale.json", "{}", "not a locale.json"},
		{"zh-Hans.json", "{}", "zh-CN.json"},
		{"fr.toml", "[labels", "fr.toml"},
		{"ja.toml", "[labels]\nemail = 123", "ja.toml"},
		{"zh-CN.toml", "", "zh-CN.json"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writeCatalogFile(t, dir, "zh-CN.json", "{}")
			writeCatalogFile(t, dir, tc.name, tc.data)
			catalogs, err := LoadCatalogs(dir)
			if err == nil || !strings.Contains(err.Error(), tc.want) || catalogs != nil {
				t.Fatalf("got %v, %v; expected an error mentioning %q", catalogs, err, tc.want)
			}
		})
	}
	dir := t.TempDir()
	if catalogs, err := LoadCatalogs(dir); err != nil || catalogs == nil || len(catalogs) != 0 {
		t.Fatalf("empty directory got %v, %v", catalogs, err)
	}
	if catalogs, err := LoadCatalogs(filepath.Join(dir, "missing")); !errors.Is(err, fs.ErrNotExist) || catalogs != nil {
		t.Fatalf("missing directory got %v, %v", catalogs, err)
	}
}

func writeCatalogFile(t *testing.T, dir, name, contents string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}
