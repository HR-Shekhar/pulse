| Verb | Meaning | Typical use |
|------|---------|-------------|
| `%s` | string (or type with `String()`) | URL, `"UP"`, Duration |
| `%v` | default format for any value | error, structs, “just print it” |
| `%d` | integer (decimal) | status code `200`, `response_ms` |
| `%f` | float | rarely needed early |
| `%t` | bool | `true` / `false` |
| `%q` | quoted string | debugging strings with spaces |
| `%+v` | struct with field names | debug dumps |
| `%T` | type of the value | debugging (“what type is this?”) |
| `%%` | literal `%` | print a percent sign |
