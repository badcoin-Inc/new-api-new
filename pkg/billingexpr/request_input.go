package billingexpr

import (
	"strings"

	"github.com/expr-lang/expr/ast"
)

// TrimRequestInputForStorage keeps the request data needed by a frozen
// billing expression while avoiding credentials and unrelated headers.
func TrimRequestInputForStorage(exprStr string, input RequestInput) RequestInput {
	trimmed := RequestInput{}
	if len(input.Body) > 0 {
		trimmed.Body = append([]byte(nil), input.Body...)
	}
	trimmed.Headers = TrimRequestHeadersForStorage(exprStr, input.Headers)
	return trimmed
}

// TrimRequestHeadersForStorage keeps only non-sensitive headers needed by a
// frozen billing expression.
func TrimRequestHeadersForStorage(exprStr string, headers map[string]string) map[string]string {
	headerNames, dynamicHeaders := requestHeaderReferences(exprStr)
	var trimmed map[string]string
	for key, value := range headers {
		key = strings.TrimSpace(key)
		if key == "" || strings.TrimSpace(value) == "" || isSensitiveRequestHeader(key) {
			continue
		}
		lowerKey := strings.ToLower(key)
		if !dynamicHeaders {
			if _, ok := headerNames[lowerKey]; !ok {
				continue
			}
		}
		if trimmed == nil {
			trimmed = make(map[string]string)
		}
		trimmed[key] = value
	}
	return trimmed
}

func requestHeaderReferences(exprStr string) (map[string]struct{}, bool) {
	headerNames := make(map[string]struct{})
	program, err := CompileFromCache(exprStr)
	if err != nil {
		return headerNames, true
	}
	dynamicHeaders := false
	ast.Find(program.Node(), func(node ast.Node) bool {
		var name string
		var arguments []ast.Node
		switch call := node.(type) {
		case *ast.CallNode:
			callee, ok := call.Callee.(*ast.IdentifierNode)
			if !ok {
				return false
			}
			name = callee.Value
			arguments = call.Arguments
		case *ast.BuiltinNode:
			name = call.Name
			arguments = call.Arguments
		default:
			return false
		}
		if name != "header" {
			return false
		}
		if len(arguments) != 1 {
			dynamicHeaders = true
			return false
		}
		literal, ok := arguments[0].(*ast.StringNode)
		if !ok || strings.TrimSpace(literal.Value) == "" {
			dynamicHeaders = true
			return false
		}
		headerNames[strings.ToLower(strings.TrimSpace(literal.Value))] = struct{}{}
		return false
	})
	return headerNames, dynamicHeaders
}

func isSensitiveRequestHeader(key string) bool {
	key = strings.ToLower(strings.TrimSpace(key))
	if key == "authorization" || key == "cookie" || key == "set-cookie" || key == "proxy-authorization" || key == "x-api-key" {
		return true
	}
	return strings.HasPrefix(key, "x-forwarded-") ||
		strings.HasPrefix(key, "cf-") ||
		strings.HasPrefix(key, "sec-")
}
