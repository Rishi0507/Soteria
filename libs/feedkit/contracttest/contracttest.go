// Package contracttest validates produced events against the JSON Schema in
// /contracts so schema drift fails in CI, per PRD §6 item 5.
package contracttest

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

// SchemaPath locates contracts/events/<name> by walking up from the test's
// working directory until a /contracts directory is found.
func SchemaPath(t testing.TB, name string) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		p := filepath.Join(dir, "contracts", "events", name)
		if _, err := os.Stat(p); err == nil {
			return p
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("contracttest: %s not found above %s", name, dir)
		}
		dir = parent
	}
}

// Compile loads and compiles a schema file.
func Compile(t testing.TB, path string) *jsonschema.Schema {
	t.Helper()
	c := jsonschema.NewCompiler()
	c.AssertFormat()
	s, err := c.Compile(filepath.ToSlash(path))
	if err != nil {
		t.Fatalf("contracttest: compile %s: %v", path, err)
	}
	return s
}

// Validate fails the test if payload does not satisfy the schema.
func Validate(t testing.TB, schema *jsonschema.Schema, payload []byte) {
	t.Helper()
	if err := Check(schema, payload); err != nil {
		t.Fatalf("contracttest: %v\npayload: %s", err, payload)
	}
}

// Check returns a descriptive error if payload violates schema.
func Check(schema *jsonschema.Schema, payload []byte) error {
	v, err := jsonschema.UnmarshalJSON(bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("invalid JSON: %w", err)
	}
	if err := schema.Validate(v); err != nil {
		var ve *jsonschema.ValidationError
		if ok := asValidationError(err, &ve); ok {
			out, _ := json.MarshalIndent(ve.DetailedOutput(), "", "  ")
			return fmt.Errorf("schema violation:\n%s", out)
		}
		return err
	}
	return nil
}

func asValidationError(err error, target **jsonschema.ValidationError) bool {
	ve, ok := err.(*jsonschema.ValidationError)
	if ok {
		*target = ve
	}
	return ok
}
