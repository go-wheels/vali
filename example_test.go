package vali_test

import (
	"context"
	"fmt"
	"log"

	"github.com/go-wheels/vali"
)

func ExampleValidateStruct() {
	type signUp struct {
		Email string `json:"email" validate:"bail|required|email" label:"邮箱"`
	}
	fields, err := vali.ValidateStruct(context.Background(), signUp{}, vali.StructOptions{Locale: "zh-CN"})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(fields.Valid())
	fmt.Println(fields.ForPath("email")[0].Message)
	// Output:
	// false
	// 请填写 邮箱。
}

func ExampleValidateField() {
	for _, code := range []string{"042", "AB"} {
		fields, err := vali.ValidateField(context.Background(), code, `bail|required|regex:'^\d{2,4}$'`)
		if err != nil {
			log.Fatal(err)
		}
		fmt.Println(code, fields.Valid())
	}
	// Output:
	// 042 true
	// AB false
}

func ExampleValidator_ValidateStruct() {
	type signUp struct {
		Email string `json:"email" validate:"bail|required|email" label:"form.email"`
	}
	v, err := vali.New(vali.Config{Catalogs: map[string]vali.Catalog{
		"en":    {Labels: map[string]string{"form.email": "Email"}},
		"zh-CN": {Labels: map[string]string{"form.email": "邮箱"}},
	}})
	if err != nil {
		log.Fatal(err)
	}
	for _, locale := range []string{"zh-CN", "en"} {
		fields, err := v.ValidateStruct(context.Background(), signUp{}, vali.StructOptions{Locale: locale})
		if err != nil {
			log.Fatal(err)
		}
		fmt.Println(fields[0].Path, fields[0].Message)
	}
	// Output:
	// email 请填写 邮箱。
	// email Email is required.
}

func ExampleValidator_ValidateFieldWithRules() {
	v, err := vali.New(vali.Config{})
	if err != nil {
		log.Fatal(err)
	}
	rules, err := v.CompileRules(`bail|required|regex:'^\d{2,4}$'`)
	if err != nil {
		log.Fatal(err)
	}
	for _, code := range []string{"042", "AB"} {
		fields, err := v.ValidateFieldWithRules(context.Background(), code, rules)
		if err != nil {
			log.Fatal(err)
		}
		fmt.Println(code, fields.Valid())
	}
	// Output:
	// 042 true
	// AB false
}

func ExampleDatabaseLookupFunc() {
	users := map[string]bool{"alice@example.org": true}
	v, err := vali.New(vali.Config{DatabaseLookup: vali.DatabaseLookupFunc(func(_ context.Context, query vali.DatabaseQuery) (bool, error) {
		if query.Table == "users" && query.Column == "email" {
			address, _ := query.Value.(string)
			return users[address], nil
		}
		return false, fmt.Errorf("unknown lookup target")
	})})
	if err != nil {
		log.Fatal(err)
	}
	type signUp struct {
		Email string `json:"email" validate:"bail|required|email|unique:users,email"`
	}
	fields, err := v.ValidateStruct(context.Background(), signUp{"alice@example.org"})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(fields[0].Rule)
	// Output: unique
}
