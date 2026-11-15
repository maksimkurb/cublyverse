package main

import (
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"

	"github.com/maksimkurb/cublyverse/api"
)

func TestOpenAPIDocumentationIsComplete(t *testing.T) {
	spec, err := openapi3.NewLoader().LoadFromData(api.OpenAPISpec)
	if err != nil {
		t.Fatalf("load api/openapi.yaml: %v", err)
	}
	if problems := openAPIDocumentationProblems(spec); len(problems) > 0 {
		t.Fatalf("api/openapi.yaml has undocumented operations or schemas:\n%s", strings.Join(problems, "\n"))
	}
}

func TestOpenAPIDocumentationProblems(t *testing.T) {
	const document = `
openapi: 3.0.3
info:
  title: Test
  version: 1.0.0
paths:
  /health:
    get:
      operationId: getHealth
      responses:
        "200":
          description: OK
components:
  schemas:
    Health:
      type: object
      properties:
        status:
          type: string
`
	spec, err := openapi3.NewLoader().LoadFromData([]byte(document))
	if err != nil {
		t.Fatalf("load test document: %v", err)
	}

	problems := openAPIDocumentationProblems(spec)
	joined := strings.Join(problems, "\n")
	for _, want := range []string{
		"GET /health: missing summary",
		"GET /health: missing description",
		"GET /health: missing tag",
		"schema Health: missing description",
		"schema Health property status: missing description",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("problems do not contain %q:\n%s", want, joined)
		}
	}
}
