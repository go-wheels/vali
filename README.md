# vali

Laravel 风格的 Go 表单校验库。支持结构体标签、单值规则表达式和可复用的自定义规则，内置英文与简体中文消息。

需要 Go 1.22 或更高版本。仓库内的可运行示例见 [example_test.go](example_test.go)，可用 `go test -run '^Example' -v` 查看输出。

## 快速开始

用 `validate` 声明规则，用 `prepare` 在校验前清理输入。下面的程序会输出 `email 请填写 邮箱。`。

~~~go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/go-wheels/vali"
)

type SignUp struct {
    Email string `json:"email" label:"邮箱" prepare:"trim" validate:"bail|required|email"`
}

func main() {
    fields, err := vali.ValidateStruct(context.Background(), SignUp{Email: " "},
        vali.StructOptions{Locale: "zh-CN"})
    if err != nil {
        log.Fatal(err)
    }
    for _, field := range fields {
        fmt.Println(field.Path, field.Message)
    }
}
~~~

| 要做什么 | 调用方式 |
| --- | --- |
| 按标签校验结构体 | `vali.ValidateStruct(ctx, form)` |
| 按表达式校验单个值 | `vali.ValidateField(ctx, value, "required\|email")` |
| 重复使用同一表达式 | 用 `v.CompileRules(expression)` 编译一次，再调用 `v.ValidateFieldWithRules(ctx, value, rules)` |
| 直接执行一条规则 | `vali.Email(value)`、`vali.CNMobile(value)` 等 |

前两种方式可直接使用包级函数。需要自定义规则、翻译或数据库查询时，用 `vali.New(config)` 创建并复用 `Validator`，再调用对应方法。

### 处理结果

`ValidateStruct`、`ValidateField` 和 `ValidateFieldWithRules` 都返回 `(FieldErrors, error)`。先处理 `error`，它报告规则配置、预处理、数据库查询和上下文等执行错误。`err == nil` 时，`fields.Valid()` 表示全部通过，`fields.ForPath("email")` 可按路径取出错误。每条错误包含 `Path`、`Rule` 和可直接展示的 `Message`。

独立规则函数返回 `(bool, error)`。规则失败可用 `errors.Is(err, vali.ErrInvalidValue)` 识别。

接入 Ant Design Form 时，把 `fields.AntDesignFields()` 编码为 JSON，再在前端调用 `form.setFields(payload)`。同一字段的消息会合并，`users.0.email` 会转成 `["users", 0, "email"]`，空结果编码为 `[]`。

## 规则与字段路径

| 类别 | 内置规则 |
| --- | --- |
| 必填与流程 | `required`、`nullable`、`bail`、`dive` |
| 类型与格式 | `string`、`boolean`、`array`、`integer`、`numeric`、`email`、`url`、`regex`、`cn_mobile`、`cn_id_card` |
| 范围与集合 | `min`、`max`、`between`、`size`、`in`、`not_in` |
| 关联字段 | `same`、`different`、`confirmed`、`required_if`、`required_unless`、`required_with`、`required_without` |
| 数据库 | `unique`、`exists` |

用 `|` 串联规则，用 `:` 引出参数，用 `,` 分隔参数。参数包含这些分隔符或需要保留首尾空格时，用单引号包住整个参数。引号内的 `''` 表示一个字面单引号。程序组装单条规则时，可用 `v.CompileRule("in", "a|b,c", "O'Reilly")` 直接传参。

~~~go
// Go 原始字符串保留正则里的反斜杠。
fields, err := vali.ValidateField(ctx, "042", `bail|required|regex:'^\d{2,4}$'`)

// 结构体标签会先按 Go 标签语法解码，反斜杠需要转义。
type Verification struct {
    Code string `validate:"bail|required|regex:'^\\d{2,4}$'"`
}
~~~

- **空值**。`required` 拒绝 `nil`、空白字符串和空集合，接受 `0` 和 `false`。在规则表达式中，空白字符串会跳过普通规则。`nullable` 让 `nil` 跳过普通规则，必填与条件必填仍会执行。`bail` 在当前字段的第一条失败规则后停止。
- **集合与范围**。`dive` 将后续规则应用到数组或切片元素。范围规则按 Unicode 码点数、集合元素数或数值大小比较。规则链包含 `numeric` 或 `integer` 时，数字字符串按数值比较。
- **字段路径**。优先使用 `json` 标签名，再用 Go 字段名。嵌套字段用点号连接，列表索引从零开始。未导出字段以及标记为 `json:"-"` 或 `validate:"-"` 的字段会跳过。
- **关联字段**。`same:password` 引用同级字段，`same:/account.email` 从根结构体查找。校验单值时，用 `FieldOptions.LookupField` 提供关联字段。单值错误路径默认为 `value`，可用 `FieldOptions.Path` 和 `Label` 指定路径和显示名称。
- **中国大陆号码**。`cn_mobile` 检查 11 位手机号格式。`cn_id_card` 检查 18 位居民身份证号的省级地址前缀、出生日期和校验码。

## 按需配置

### 预处理与临时规则

`prepare` 内置 `trim`、`lowercase`、`uppercase`，也可用 `dive|trim` 处理集合元素。标签转换在副本上执行，跨字段比较和数据库规则使用转换后的值。用 `Config.PreprocessorFactories` 注册自定义转换，或用 `StructOptions.PrepareForm` 和 `FieldOptions.PrepareValue` 设置单次回调。

`StructOptions.ExtraRules` 按路径追加本次校验的规则，支持 `users.0.email` 和 `users.*.email`。标签规则先执行，同时匹配时先执行通配规则，再执行具体路径上的规则。可用 `v.CompileRule` 获取规则，也可用 `vali.NewRule(name, spec)` 包装临时检查。

### 翻译与语言选择

用 `StructOptions.Locale` 或 `FieldOptions.Locale` 显式选择语言。HTTP 服务也可把请求头交给 `vali.WithAcceptLanguage(r.Context(), r.Header.Values("Accept-Language")...)`，按权重选择语言。翻译可依次回退到父级语言代码（`sr-Latn-RS` → `sr-Latn` → `sr`）、`Config.FallbackLocale` 和英文。

用 `vali.LoadCatalogs("locales")` 读取语言目录，将结果传给 `Config.Catalogs`。每种语言一个 TOML 或 JSON 文件，例如 `locales/zh-CN.toml`。文件包含未知配置键时，加载错误会指出文件名和键名。

~~~toml
[labels]
"form.email" = "邮箱"

[messages]
required = "请填写 :attribute。"

[field_messages.email]
email = "请检查 :attribute 的格式。"

[form_labels.signup]
email = "注册邮箱"

[form_messages.signup.email]
required = "请填写用于接收通知的:attribute。"
~~~

`label:"form.email"` 引用 `labels` 中的名称。表单可通过 `ValidationFormAlias() string` 返回固定别名，例如 `"signup"`，并使用对应的 `form_labels` 和 `form_messages`。同一种语言内，消息依次查找表单、字段和通用模板，精确路径优先于通配路径。`:attribute` 和 `:other` 分别替换为当前字段和关联字段的名称。

### 自定义规则

在 `Config.RuleFactories` 中以规则名注册 `RuleFactory`。工厂接收参数并返回 `RuleSpec`，即可在标签和表达式中使用该规则。`RuleSpec.Check` 接收 `RuleInput`，通过时返回 `nil, nil`，规则失败时返回 `*Violation`，无法完成检查时返回 `error`。通过 `RuleInput.LookupField` 访问关联字段。需要处理被普通规则跳过的值时，设置 `RuleSpec.Implicit`。

### 数据库规则

用 `vali.NewSQLLookup(db, vali.PostgreSQL)` 创建查询适配器，再赋给 `Config.DatabaseLookup`。也支持 `vali.MySQL`、`vali.SQLite`，或通过 `vali.DatabaseLookupFunc` 接入自有查询。创建验证器和适配器时先处理各自返回的错误。

~~~go
type UpdateUser struct {
    Email  string `json:"email" validate:"bail|required|email|unique:users,email"`
    RoleID int    `json:"role_id" validate:"exists:roles,id"`
}

fields, err := v.ValidateStruct(ctx, form, vali.StructOptions{
    UniqueExclusions: map[string]vali.RowExclusion{
        "email": {Column: "id", Value: authorizedUserID},
    },
})
~~~

`form` 为待校验的 `UpdateUser`。更新数据时，在 `UniqueExclusions` 中用实际字段路径指定当前记录，记录 ID 由服务端确认。并发写入时由数据库唯一约束保证唯一性。
