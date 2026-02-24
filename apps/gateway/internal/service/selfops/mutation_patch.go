package selfops

import (
	"fmt"
	"strings"
)

func applyJSONReplaceOperation(root interface{}, path string, value interface{}) (interface{}, error) {
	trimmedPath := strings.TrimSpace(path)
	if trimmedPath == "" {
		return cloneJSONValue(value), nil
	}
	tokens, err := parseJSONPointerTokens(trimmedPath)
	if err != nil {
		return nil, err
	}
	current := cloneJSONValue(root)
	return setJSONPointerMapValue(current, tokens, cloneJSONValue(value), false)
}

func applyJSONPatchOperations(root interface{}, operations []JSONPatchOperation) (interface{}, error) {
	current := cloneJSONValue(root)
	for _, op := range operations {
		next, err := applyJSONPatchOperation(current, op)
		if err != nil {
			return nil, err
		}
		current = next
	}
	return current, nil
}

func applyJSONPatchOperation(root interface{}, op JSONPatchOperation) (interface{}, error) {
	opType := strings.ToLower(strings.TrimSpace(op.Op))
	path := strings.TrimSpace(op.Path)

	switch opType {
	case "replace":
		return applyJSONReplaceOperation(root, path, op.Value)
	case "add":
		if path == "" {
			return cloneJSONValue(op.Value), nil
		}
		tokens, err := parseJSONPointerTokens(path)
		if err != nil {
			return nil, err
		}
		current := cloneJSONValue(root)
		return setJSONPointerMapValue(current, tokens, cloneJSONValue(op.Value), true)
	case "remove":
		if path == "" {
			return nil, fmt.Errorf("remove operation does not support empty path")
		}
		tokens, err := parseJSONPointerTokens(path)
		if err != nil {
			return nil, err
		}
		current := cloneJSONValue(root)
		return removeJSONPointerMapValue(current, tokens)
	default:
		return nil, fmt.Errorf("unsupported json_patch op: %s", op.Op)
	}
}

func setJSONPointerMapValue(root interface{}, tokens []string, value interface{}, allowCreate bool) (interface{}, error) {
	if len(tokens) == 0 {
		return cloneJSONValue(value), nil
	}
	if root == nil {
		if !allowCreate {
			return nil, fmt.Errorf("path does not exist")
		}
		root = map[string]interface{}{}
	}
	current, ok := root.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("json pointer target is not an object")
	}

	for index := 0; index < len(tokens)-1; index++ {
		token := tokens[index]
		nextValue, exists := current[token]
		if !exists || nextValue == nil {
			if !allowCreate {
				return nil, fmt.Errorf("path does not exist: /%s", strings.Join(tokens[:index+1], "/"))
			}
			child := map[string]interface{}{}
			current[token] = child
			current = child
			continue
		}
		nextMap, ok := nextValue.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("json pointer path segment %q is not an object", token)
		}
		current = nextMap
	}

	lastToken := tokens[len(tokens)-1]
	if !allowCreate {
		if _, exists := current[lastToken]; !exists {
			return nil, fmt.Errorf("path does not exist: /%s", strings.Join(tokens, "/"))
		}
	}
	current[lastToken] = value
	return root, nil
}

func removeJSONPointerMapValue(root interface{}, tokens []string) (interface{}, error) {
	if len(tokens) == 0 {
		return nil, fmt.Errorf("path is required")
	}
	current, ok := root.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("json pointer target is not an object")
	}

	for index := 0; index < len(tokens)-1; index++ {
		token := tokens[index]
		nextValue, exists := current[token]
		if !exists {
			return nil, fmt.Errorf("path does not exist: /%s", strings.Join(tokens[:index+1], "/"))
		}
		nextMap, ok := nextValue.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("json pointer path segment %q is not an object", token)
		}
		current = nextMap
	}

	lastToken := tokens[len(tokens)-1]
	if _, exists := current[lastToken]; !exists {
		return nil, fmt.Errorf("path does not exist: /%s", strings.Join(tokens, "/"))
	}
	delete(current, lastToken)
	return root, nil
}

func parseJSONPointerTokens(path string) ([]string, error) {
	if strings.TrimSpace(path) == "" {
		return []string{}, nil
	}
	if !strings.HasPrefix(path, "/") {
		return nil, fmt.Errorf("json pointer must start with /")
	}
	rawTokens := strings.Split(path[1:], "/")
	tokens := make([]string, 0, len(rawTokens))
	for _, rawToken := range rawTokens {
		token := strings.ReplaceAll(rawToken, "~1", "/")
		token = strings.ReplaceAll(token, "~0", "~")
		tokens = append(tokens, token)
	}
	return tokens, nil
}
