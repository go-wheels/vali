package vali

import (
	"context"
	"net/http"
	"testing"
)

func TestAcceptLanguageNegotiation(t *testing.T) {
	v := newTestValidator(t, Config{
		FallbackLocale: "de-Latn-DE",
		Catalogs: map[string]Catalog{
			"de-Latn":    {Messages: map[string]string{"required": "German"}},
			"fr":         {Messages: map[string]string{"required": "French"}},
			"fr-CA":      {Messages: map[string]string{"required": "Canadian French"}},
			"pt-BR":      {Messages: map[string]string{"required": "Brazilian Portuguese"}},
			"zh-CN":      {Messages: map[string]string{"required": "Simplified Chinese"}},
			"sr-Latn":    {Messages: map[string]string{"required": "Latin Serbian"}},
			"sr-Latn-RS": {Labels: map[string]string{"name": "Serbian label"}},
		},
	})

	for _, tc := range []struct {
		name, header, manual, want string
	}{
		{name: "quality", header: "en;q=0.4, fr-CA;q=0.9", want: "Canadian French"},
		{name: "stable order", header: "FR-CA, en", want: "Canadian French"},
		{name: "skip unsupported", header: "ja, zh-Hans;q=0.8, en;q=0.5", want: "Simplified Chinese"},
		{name: "base catalog", header: "fr-BE", want: "French"},
		{name: "script catalog", header: "sr-Latn-RS", want: "Latin Serbian"},
		{name: "manual parent catalog", manual: "sr-Latn-BA", want: "Latin Serbian"},
		{name: "regional catalog", header: "pt", want: "Brazilian Portuguese"},
		{name: "specific exclusion", header: "pt, pt-BR;q=0, en;q=0.5", want: "value is required."},
		{name: "specific weight", header: "pt, pt-BR;q=0.1, en;q=0.5", want: "value is required."},
		{name: "specific preference", header: "fr;q=0, fr-CA;q=0.5", want: "Canadian French"},
		{name: "invalid entries", header: "fr;q=1.1, fr-CA;q=nope, en;q=0.5", want: "value is required."},
		{name: "wildcard", header: "ja, *;q=0.9, fr;q=0.5", want: "German"},
		{name: "wildcard exclusion", header: "de;q=0, *;q=0.5", want: "value is required."},
		{name: "wildcard skips explicit matches", header: "de;q=0.1, *;q=0.9", want: "value is required."},
		{name: "unmatched fallback", header: "ja, es;q=0.5", want: "German"},
		{name: "empty header", want: "German"},
		{name: "manual locale", header: "fr-CA", manual: "ZH_cn", want: "Simplified Chinese"},
		{name: "manual fallback", header: "fr-CA", manual: "ja", want: "German"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := WithAcceptLanguage(context.Background(), tc.header)
			fields, err := v.ValidateField(ctx, "", "required", FieldOptions{Locale: tc.manual})
			if err != nil || len(fields) != 1 || fields[0].Message != tc.want {
				t.Fatalf("fields = %v, err = %v; want %q", fields, err, tc.want)
			}
		})
	}
}

func TestAcceptLanguageValidationEntryPoints(t *testing.T) {
	header := make(http.Header)
	header.Add("Accept-Language", "en;q=0.5")
	header.Add("Accept-Language", "zh-CN")
	ctx := WithAcceptLanguage(context.Background(), header.Values("Accept-Language")...)
	v := newTestValidator(t, Config{Catalogs: map[string]Catalog{
		"zh-CN": {Labels: map[string]string{"account.email": "邮箱"}},
	}})
	type form struct {
		Email string `json:"email" label:"account.email" validate:"required"`
	}
	fields, err := v.ValidateStruct(ctx, form{})
	if err != nil || len(fields) != 1 || fields[0].Label != "邮箱" || fields[0].Message != "请填写 邮箱。" {
		t.Fatalf("ValidateStruct = %v, %v", fields, err)
	}
	fields, err = ValidateStruct(ctx, form{}, StructOptions{Locale: "en"})
	if err != nil || len(fields) != 1 || fields[0].Message != "account.email is required." {
		t.Fatalf("ValidateStruct with explicit locale = %v, %v", fields, err)
	}
	fields, err = ValidateField(ctx, "", "required")
	if err != nil || len(fields) != 1 || fields[0].Message != "请填写 value。" {
		t.Fatalf("ValidateField = %v, %v", fields, err)
	}
	rules, err := v.CompileRules("required")
	if err != nil {
		t.Fatal(err)
	}
	fields, err = v.ValidateFieldWithRules(ctx, "", rules)
	if err != nil || len(fields) != 1 || fields[0].Message != "请填写 value。" {
		t.Fatalf("ValidateFieldWithRules = %v, %v", fields, err)
	}
	fields, err = v.ValidateFieldWithRules(ctx, "", rules, FieldOptions{Locale: "en"})
	if err != nil || len(fields) != 1 || fields[0].Message != "value is required." {
		t.Fatalf("ValidateFieldWithRules with explicit locale = %v, %v", fields, err)
	}
}
