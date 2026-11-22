package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

const schemaVersion = "1"

func schemaFor(command string, ndjson bool) map[string]any {
	schema := map[string]any{
		"$schema":              "https://json-schema.org/draft/2020-12/schema",
		"type":                 "object",
		"properties":           map[string]any{"schema_version": map[string]any{"const": schemaVersion}},
		"required":             []string{"schema_version"},
		"additionalProperties": true,
	}
	properties := schema["properties"].(map[string]any)
	addArray := func(name string) {
		properties[name] = map[string]any{"type": "array", "items": map[string]any{"type": "object"}}
	}
	addString := func(name string) { properties[name] = map[string]any{"type": "string"} }
	addStringArray := func(name string) {
		properties[name] = map[string]any{"type": "array", "items": map[string]any{"type": "string"}}
	}
	if ndjson {
		switch command {
		case "snapshot", "recent":
			addString("session_id")
		case "export":
			properties["seq"] = map[string]any{"type": "integer"}
			addString("role")
			addString("provider")
		}
		return schema
	}
	key := map[string]string{
		"snapshot": "sessions",
		"turns":    "turns",
		"window":   "reports",
		"export":   "events",
		"recent":   "sessions",
	}[command]
	if key != "" {
		addArray(key)
		if command != "window" {
			schema["required"] = []string{"schema_version", key}
		}
	}
	switch command {
	case "recent":
		addString("generated_at")
	case "stats":
		addString("since")
		properties["sessions_scanned"] = map[string]any{"type": "integer"}
		addArray("findings")
		addArray("spend")
		addArray("rhythm")
	case "config":
		addString("server")
		addString("token")
		addString("interval")
		addString("scan_lookback")
		addStringArray("providers")
		addStringArray("exclude")
		addStringArray("redact_paths")
	case "telemetry":
		properties["config_enabled"] = map[string]any{"type": "boolean"}
		properties["listener_accepting"] = map[string]any{"type": "boolean"}
	case "claude-limits":
		properties["enabled"] = map[string]any{"type": "boolean"}
		properties["signed_in"] = map[string]any{"type": "boolean"}
		addArray("windows")
	case "whoami":
		addString("server")
		addString("account")
		addString("machine")
	case "version":
		addString("version")
		addString("commit")
		addString("date")
	case "daemon":
		addStringArray("lines")
	case "otel-reconcile":
		addString("report")
	}
	return schema
}

func hasFlag(args []string, name string) bool {
	for _, arg := range args {
		if arg == name || strings.HasPrefix(arg, name+"=") {
			return true
		}
	}
	return false
}

func addSchemaVersion(value any) (any, error) {
	switch value := value.(type) {
	case map[string]any:
		value["schema_version"] = schemaVersion
		return value, nil
	case []any:
		return map[string]any{"schema_version": schemaVersion, "sessions": value}, nil
	default:
		return nil, fmt.Errorf("JSON output must be an object or array")
	}
}

func transformJSONOutput(command string, args []string, output []byte) ([]byte, error) {
	ndjson := hasFlag(args, "--ndjson")
	if command == "export" && !hasFlag(args, "--json") {
		ndjson = true
	}
	if ndjson || command == "export" {
		var out bytes.Buffer
		lines := bytes.Split(bytes.TrimSpace(output), []byte{'\n'})
		var values []any
		for _, line := range lines {
			if len(bytes.TrimSpace(line)) == 0 {
				continue
			}
			var value any
			if err := json.Unmarshal(line, &value); err != nil {
				return nil, err
			}
			values = append(values, value)
		}
		if command == "snapshot" || command == "recent" {
			for _, value := range values {
				versioned, err := addSchemaVersion(value)
				if err != nil {
					return nil, err
				}
				if err := json.NewEncoder(&out).Encode(versioned); err != nil {
					return nil, err
				}
			}
			return out.Bytes(), nil
		}
		if command == "export" && hasFlag(args, "--json") {
			return encodeJSON(map[string]any{"schema_version": schemaVersion, "events": values})
		}
		for _, value := range values {
			versioned, err := addSchemaVersion(value)
			if err != nil {
				return nil, err
			}
			if err := json.NewEncoder(&out).Encode(versioned); err != nil {
				return nil, err
			}
		}
		return out.Bytes(), nil
	}
	var value any
	if err := json.Unmarshal(output, &value); err != nil {
		return nil, err
	}
	if items, ok := value.([]any); ok {
		key := map[string]string{"snapshot": "sessions", "turns": "turns", "window": "reports", "export": "events"}[command]
		if key == "" {
			key = "items"
		}
		value = map[string]any{"schema_version": schemaVersion, key: items}
	} else {
		value, _ = addSchemaVersion(value)
	}
	return encodeJSON(value)
}

func encodeJSON(value any) ([]byte, error) {
	var out bytes.Buffer
	enc := json.NewEncoder(&out)
	enc.SetIndent("", "  ")
	if err := enc.Encode(value); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func writeSchema(command string, args []string, stdout io.Writer) int {
	enc := json.NewEncoder(stdout)
	enc.SetIndent("", "  ")
	ndjson := hasFlag(args, "--ndjson") || (command == "export" && !hasFlag(args, "--json"))
	if err := enc.Encode(schemaFor(command, ndjson)); err != nil {
		return 1
	}
	return 0
}
