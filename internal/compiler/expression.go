package compiler

import (
	"encoding/json"
	"github.com/Karthikeyan49/Framwork/internal/metadata"
	"strings"
)

type expressionCheck func(metadata.Expression, string, string) string

func expressionChecker(fields map[string]Field, owners map[string]string, add func(string, string, string)) expressionCheck {
	var check expressionCheck
	check = func(e metadata.Expression, owner, path string) string {
		if e.Op == "field" {
			f, ok := fields[e.FieldRef]
			if !ok || owners[e.FieldRef] != owner {
				add(path, "expression_field", "Expression field must resolve within its entity")
				return "invalid"
			}
			return f.Type
		}
		if e.Op == "literal" {
			var v any
			decoder := json.NewDecoder(strings.NewReader(string(e.Value)))
			decoder.UseNumber()
			if decoder.Decode(&v) != nil {
				return "invalid"
			}
			switch v := v.(type) {
			case nil:
				return "null"
			case bool:
				return "bool"
			case string:
				return "string"
			case json.Number:
				if strings.ContainsAny(v.String(), ".eE") {
					return "decimal"
				}
				return "integer"
			}
			return "invalid"
		}
		arity := 2
		if e.Op == "not" || e.Op == "is_null" {
			arity = 1
		}
		if e.Op == "and" || e.Op == "or" {
			if len(e.Args) < 2 {
				add(path, "expression_arity", "Boolean groups require at least two arguments")
			}
		} else if len(e.Args) != arity {
			add(path, "expression_arity", "Operator argument count is invalid")
		}
		types := []string{}
		for _, a := range e.Args {
			types = append(types, check(a, owner, path+"/args"))
		}
		if len(types) == 0 {
			return "invalid"
		}
		switch e.Op {
		case "and", "or", "not":
			for _, typ := range types {
				if typ != "bool" {
					add(path, "expression_type", "Boolean operators require boolean arguments")
				}
			}
		case "is_null":
		case "eq", "neq":
			if len(types) == 2 && !compatible(types[0], types[1]) {
				add(path, "expression_type", "Equality operands must have compatible types")
			}
		case "gt", "gte", "lt", "lte":
			if len(types) == 2 && (!compatible(types[0], types[1]) || !contains([]string{"integer", "decimal", "money", "date", "datetime"}, types[0])) {
				add(path, "expression_type", "Ordering requires compatible ordered types")
			}
		case "contains":
			if len(types) != 2 || types[0] != "string" || types[1] != "string" {
				add(path, "expression_type", "Contains requires two strings")
			}
		case "in", "not_in":
			add(path, "unavailable_operator", "List literal support is unavailable in schema 1.0.0")
		default:
			add(path, "unknown_operator", "Operator is unavailable")
		}
		return "bool"
	}
	return check
}
func compatible(a, b string) bool {
	return a == b || a == "null" || b == "null" || (a == "decimal" && b == "integer") || (a == "integer" && b == "decimal") || (a == "enum" && b == "string") || (a == "string" && b == "enum")
}
