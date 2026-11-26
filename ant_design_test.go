package vali

import (
	"context"
	"encoding/json"
	"testing"
)

func TestAntDesignFields(t *testing.T) {
	v := newTestValidator(t, Config{})
	type user struct {
		Email string `json:"email" label:"邮箱" validate:"email|min:8"`
	}
	type form struct {
		Users []user `json:"users"`
		Name  string `json:"name" label:"姓名" validate:"required"`
	}
	failures, err := v.ValidateStruct(context.Background(), form{Users: []user{{Email: "bad"}}}, StructOptions{Locale: "zh-CN"})
	if err != nil || len(failures) != 3 {
		t.Fatalf("got %v, %v; expected three failures", failures, err)
	}
	// Messages for a field stay together even if its failures are interleaved.
	failures[1], failures[2] = failures[2], failures[1]
	data, err := json.Marshal(failures.AntDesignFields())
	want := `[{"name":["users",0,"email"],"errors":["邮箱 必须是有效的电子邮箱地址。","邮箱 必须至少为 8。"]},{"name":["name"],"errors":["请填写 姓名。"]}]`
	if err != nil || string(data) != want {
		t.Fatalf("got %s, %v; want %s", data, err, want)
	}
}

func TestAntDesignFieldsEmptyAndPathSegments(t *testing.T) {
	data, err := json.Marshal(FieldErrors(nil).AntDesignFields())
	if err != nil || string(data) != "[]" {
		t.Fatalf("got %s, %v; want []", data, err)
	}

	failures := FieldErrors{{Path: "groups.12.users.0.01.9007199254740991.9007199254740992", Message: "Invalid"}}
	data, err = json.Marshal(failures.AntDesignFields())
	want := `[{"name":["groups",12,"users",0,"01",9007199254740991,"9007199254740992"],"errors":["Invalid"]}]`
	if err != nil || string(data) != want {
		t.Fatalf("got %s, %v; want %s", data, err, want)
	}
}
