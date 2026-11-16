package miruro

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
)

// devalue decodes SvelteKit's flattened payload: numbers index into the
// current data array, and ["Promise", id] points at a later chunk line.
type devalue struct {
	nodes      []map[string]any
	chunks     map[int][]any
	chunkRoots map[int]any
}

func parseDevalue(body string) (*devalue, error) {
	var nodes []map[string]any
	chunks := map[int][]any{}
	for _, line := range strings.Split(body, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var object map[string]any
		decoder := json.NewDecoder(strings.NewReader(line))
		decoder.UseNumber()
		if err := decoder.Decode(&object); err != nil {
			return nil, fmt.Errorf("decode sveltekit line: %w", err)
		}
		switch object["type"] {
		case "data":
			raw, _ := object["nodes"].([]any)
			for _, item := range raw {
				if node, ok := item.(map[string]any); ok {
					nodes = append(nodes, node)
				}
			}
		case "chunk":
			id, ok := asInt(object["id"])
			if !ok {
				continue
			}
			data, ok := object["data"].([]any)
			if ok {
				chunks[id] = data
			}
		}
	}
	if nodes == nil {
		return nil, errors.New("sveltekit data missing")
	}
	return &devalue{nodes: nodes, chunks: chunks, chunkRoots: map[int]any{}}, nil
}

func (d *devalue) root(node int) (any, error) {
	if node < 0 || node >= len(d.nodes) {
		return nil, fmt.Errorf("node %d out of range", node)
	}
	data, ok := d.nodes[node]["data"].([]any)
	if !ok {
		return nil, errors.New("node data missing")
	}
	return d.resolve(data, 0, map[int]any{}), nil
}

func (d *devalue) resolve(data []any, value any, cache map[int]any) any {
	index, ok := asInt(value)
	if !ok {
		return d.deep(data, value, cache)
	}
	if index < 0 || index >= len(data) {
		return nil
	}
	if cached, ok := cache[index]; ok {
		return cached
	}
	resolved := d.deep(data, data[index], cache)
	cache[index] = resolved
	return resolved
}

func (d *devalue) deep(data []any, value any, cache map[int]any) any {
	switch typed := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, raw := range typed {
			out[key] = d.resolve(data, raw, cache)
		}
		return out
	case []any:
		if len(typed) == 2 {
			if marker, ok := typed[0].(string); ok && marker == "Promise" {
				id, _ := asInt(d.resolve(data, typed[1], cache))
				if root, ok := d.chunkRoots[id]; ok {
					return root
				}
				chunk, ok := d.chunks[id]
				if !ok {
					return nil
				}
				root := d.resolve(chunk, 0, map[int]any{})
				d.chunkRoots[id] = root
				return root
			}
		}
		out := make([]any, len(typed))
		for i, raw := range typed {
			out[i] = d.resolve(data, raw, cache)
		}
		return out
	default:
		return value
	}
}

func asInt(value any) (int, bool) {
	switch number := value.(type) {
	case json.Number:
		parsed, err := number.Int64()
		return int(parsed), err == nil
	case float64:
		return int(number), number == math.Trunc(number)
	case int:
		return number, true
	case int64:
		return int(number), true
	}
	return 0, false
}
