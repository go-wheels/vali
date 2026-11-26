package vali_test

import (
	"context"
	"errors"
	"testing"

	"github.com/go-wheels/vali"
)

func TestCNMobile(t *testing.T) {
	type mobile string
	valid := mobile("13800138000")
	for _, value := range []any{valid, &valid, "19200138000"} {
		if ok, err := vali.CNMobile(value); !ok || err != nil {
			t.Errorf("CNMobile(%v) = (%v, %v), want (true, nil)", value, ok, err)
		}
	}
	for _, value := range []any{
		"", "12800138000", "1380013800", "138001380000", "1380013800０",
		"+8613800138000", "008619200138000", "+86 13800138000", "13800138000\n", int64(13800138000),
	} {
		if ok, err := vali.CNMobile(value); ok || !errors.Is(err, vali.ErrInvalidValue) {
			t.Errorf("CNMobile(%v) = (%v, %v), want (false, ErrInvalidValue)", value, ok, err)
		}
	}
}

func TestCNIDCard(t *testing.T) {
	for _, value := range []string{"11010519491231002X", "11010519491231002x", "110105200002290013"} {
		if ok, err := vali.CNIDCard(value); !ok || err != nil {
			t.Errorf("CNIDCard(%q) = (%v, %v), want (true, nil)", value, ok, err)
		}
	}
	for _, tt := range []struct {
		name  string
		value any
	}{
		{"wrong checksum", "110105194912310021"},
		{"unsupported check character", "11010519491231002Y"},
		{"non-digit body", "11010A19491231002X"},
		{"legacy length", "110105491231002"},
		{"non-mainland prefix", "710105194912310021"},
		{"invalid leap day", "110105190002290017"},
		{"year zero", "11010500000101001X"},
		{"future birth date", "11010599991231001X"},
		{"numeric input", int64(110105200002290013)},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if ok, err := vali.CNIDCard(tt.value); ok || !errors.Is(err, vali.ErrInvalidValue) {
				t.Errorf("CNIDCard(%v) = (%v, %v), want (false, ErrInvalidValue)", tt.value, ok, err)
			}
		})
	}
}

func TestChinaRulesInValidation(t *testing.T) {
	type form struct {
		Mobile string `json:"mobile" label:"手机号" validate:"required|cn_mobile"`
		IDCard string `json:"id_card" label:"身份证号" validate:"required|cn_id_card"`
	}
	ctx := context.Background()
	fields, err := vali.ValidateStruct(ctx, form{"123", "456"}, vali.StructOptions{Locale: "zh-CN"})
	if err != nil || len(fields) != 2 {
		t.Fatalf("fields = %+v, err = %v", fields, err)
	}
	if fields[0].Path != "mobile" || fields[0].Rule != "cn_mobile" || fields[0].Message != "手机号 必须是有效的中国大陆手机号码。" {
		t.Errorf("mobile error = %+v", fields[0])
	}
	if fields[1].Path != "id_card" || fields[1].Rule != "cn_id_card" || fields[1].Message != "身份证号 必须是有效的中国大陆居民身份证号码。" {
		t.Errorf("identity number error = %+v", fields[1])
	}
	fields, err = vali.ValidateField(ctx, "bad", "cn_id_card", vali.FieldOptions{Path: "id_card", Label: "ID", Locale: "en"})
	if err != nil || len(fields) != 1 || fields[0].Message != "ID must be a valid mainland China resident identity number." {
		t.Errorf("fields = %+v, err = %v", fields, err)
	}
	fields, err = vali.ValidateStruct(ctx, form{"13800138000", "11010519491231002x"})
	if err != nil || !fields.Valid() {
		t.Errorf("fields = %+v, err = %v", fields, err)
	}
}
