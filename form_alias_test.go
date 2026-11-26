package vali

import (
	"context"
	"reflect"
	"slices"
	"testing"
)

type aliasTestUser struct {
	Email string `json:"email" label:"form.email" validate:"required|email"`
}

func (aliasTestUser) ValidationFormAlias() string { return "user" }

type aliasTestLogin struct {
	Email string          `json:"email" label:"form.email" validate:"required|email"`
	Users []aliasTestUser `json:"users"`
}

func (aliasTestLogin) ValidationFormAlias() string { return "login" }

type aliasTestSignup struct {
	Email string `json:"email" label:"form.email" validate:"required|email"`
}

func (*aliasTestSignup) ValidationFormAlias() string { return "signup" }

type aliasTestShared aliasTestSignup

func TestFormAliasMessagesFromFiles(t *testing.T) {
	dir := t.TempDir()
	writeCatalogFile(t, dir, "zh-CN.toml", `
		[labels]
		"form.email" = "邮箱"

		[field_messages.email]
		required = "通用：请填写 :attribute。"
		email = "请检查 :attribute 格式。"

		[form_labels.login]
		email = "登录邮箱"
		"users.0.email" = "首位用户邮箱"
		"users.*.email" = "用户邮箱"

		[form_labels.signup]
		email = "注册邮箱"

		[form_messages.login.email]
		required = "请输入 :attribute。"

		[form_messages.login."users.0.email"]
		required = "请输入 :attribute。"

		[form_messages.login."users.*.email"]
		required = "请输入 :attribute。"

		[form_messages.signup.email]
		required = "请输入 :attribute。"

		[form_messages.user.email]
		required = "请输入独立用户的 :attribute。"
	`)
	writeCatalogFile(t, dir, "en.json", `{
		"labels": {"form.email": "Email"},
		"field_messages": {"email": {"required": "Enter :attribute."}},
		"form_messages": {"login": {"email": {"required": "Enter your :attribute."}}},
		"form_labels": {"login": {"email": "login email"}}
	}`)
	catalogs, err := LoadCatalogs(dir)
	if err != nil {
		t.Fatal(err)
	}
	v := newTestValidator(t, Config{Catalogs: catalogs})
	catalogs["zh-CN"].FormMessages["login"]["users.*.email"]["required"] = "changed"
	catalogs["zh-CN"].FormMessages["signup"]["email"]["required"] = "changed"
	catalogs["zh-CN"].FormLabels["signup"]["email"] = "changed"

	ctx := context.Background()
	for _, tc := range []struct {
		form     any
		locale   string
		messages []string
		paths    []string
		labels   []string
	}{
		{aliasTestLogin{Users: make([]aliasTestUser, 2)}, "zh-CN",
			[]string{"请输入 登录邮箱。", "请输入 首位用户邮箱。", "请输入 用户邮箱。"},
			[]string{"email", "users.0.email", "users.1.email"}, []string{"登录邮箱", "首位用户邮箱", "用户邮箱"}},
		{aliasTestSignup{}, "zh-CN", []string{"请输入 注册邮箱。"}, []string{"email"}, []string{"注册邮箱"}},
		{&aliasTestSignup{}, "zh-CN", []string{"请输入 注册邮箱。"}, []string{"email"}, []string{"注册邮箱"}},
		{aliasTestShared{}, "zh-CN", []string{"通用：请填写 邮箱。"}, []string{"email"}, []string{"邮箱"}},
		{aliasTestLogin{Email: "bad"}, "zh-CN", []string{"请检查 登录邮箱 格式。"}, []string{"email"}, []string{"登录邮箱"}},
		{aliasTestUser{}, "zh-CN", []string{"请输入独立用户的 邮箱。"}, []string{"email"}, []string{"邮箱"}},
		{&aliasTestLogin{}, "en-US", []string{"Enter your login email."}, []string{"email"}, []string{"login email"}},
		{aliasTestSignup{}, "en-US", []string{"Enter Email."}, []string{"email"}, []string{"Email"}},
	} {
		t.Run(reflect.TypeOf(tc.form).String()+"/"+tc.locale, func(t *testing.T) {
			fields, err := v.ValidateStruct(ctx, tc.form, StructOptions{Locale: tc.locale})
			if err != nil {
				t.Fatal(err)
			}
			var messages, paths, labels []string
			for _, field := range fields {
				messages = append(messages, field.Message)
				paths = append(paths, field.Path)
				labels = append(labels, field.Label)
				if field.Params["attribute"] != field.Label {
					t.Fatalf("field label %q differs from attribute %q", field.Label, field.Params["attribute"])
				}
			}
			if !slices.Equal(messages, tc.messages) || !slices.Equal(paths, tc.paths) {
				t.Fatalf("got paths %v and messages %q; want %v and %q", paths, messages, tc.paths, tc.messages)
			}
			if !slices.Equal(labels, tc.labels) {
				t.Fatalf("got labels %q, want %q", labels, tc.labels)
			}
			if formErrors := fields.AntDesignFields(); !reflect.DeepEqual(formErrors[0].Name, []any{"email"}) {
				t.Fatalf("first Ant Design field name: %v", formErrors[0].Name)
			}
		})
	}
}

type aliasTestRelatedFields struct {
	FirstConfirm  string          `json:"first_confirm" validate:"same:users.0.email"`
	SecondConfirm string          `json:"second_confirm" validate:"same:users.1.email"`
	CodeConfirm   string          `json:"code_confirm" validate:"same:codes.0"`
	Users         []aliasTestUser `json:"users"`
	Codes         struct {
		Zero string `json:"0" label:"编号"`
	} `json:"codes"`
}

func (aliasTestRelatedFields) ValidationFormAlias() string { return "comparison" }

func TestFormLabelsForRelatedFields(t *testing.T) {
	v := newTestValidator(t, Config{Catalogs: map[string]Catalog{
		"zh-CN": {FormLabels: map[string]map[string]string{
			"comparison": {
				"first_confirm": "第一次确认",
				"users.0.email": "首位用户邮箱",
				"users.*.email": "用户邮箱",
				"codes.*":       "列表编号",
			},
		}},
	}})
	input := aliasTestRelatedFields{
		FirstConfirm: "different@example.org", SecondConfirm: "different@example.org", CodeConfirm: "B",
		Users: []aliasTestUser{{Email: "first@example.org"}, {Email: "second@example.org"}},
	}
	input.Codes.Zero = "A"
	fields, err := v.ValidateStruct(context.Background(), input, StructOptions{Locale: "zh-CN"})
	if err != nil || len(fields) != 3 {
		t.Fatalf("got %v, %v", fields, err)
	}
	if fields[0].Message != "第一次确认 必须与 首位用户邮箱 一致。" ||
		fields[1].Params["other"] != "用户邮箱" ||
		fields[2].Params["other"] != "编号" {
		t.Fatalf("unexpected related field attributes: %#v", fields)
	}
}
