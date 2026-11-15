package main

import (
	"flag"
	"fmt"
	"io"
	"sort"

	"github.com/getkin/kin-openapi/openapi3"

	"github.com/maksimkurb/cublyverse/api"
)

// runAPIDocs implements `cublyverse docs api [--check]`.
//
// The command reads the embedded api/openapi.yaml, so it checks exactly the
// document the server serves. It is used by scripts/check-docs.sh to keep the
// specification documented: an operation without a summary or a description,
// or a schema without a description, fails `make check`.
func runAPIDocs(args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("docs api", flag.ContinueOnError)
	flags.SetOutput(stderr)
	check := flags.Bool("check", false, "verify the documentation of every operation and schema")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() > 0 {
		return fmt.Errorf("docs api takes no arguments, got %q", flags.Arg(0))
	}

	spec, err := openapi3.NewLoader().LoadFromData(api.OpenAPISpec)
	if err != nil {
		return fmt.Errorf("load api/openapi.yaml: %w", err)
	}

	problems := openAPIDocumentationProblems(spec)
	if len(problems) > 0 {
		for _, problem := range problems {
			fmt.Fprintln(stderr, "error:", problem)
		}
		return fmt.Errorf("the API documentation is incomplete: %d problem(s)", len(problems))
	}
	if *check {
		return nil
	}

	fmt.Fprintf(stdout, "OpenAPI %s: %d operations, %d schemas, documentation is complete\n",
		spec.OpenAPI, countOperations(spec), countSchemas(spec))
	return printOperations(stdout, spec)
}

// openAPIDocumentationProblems collects every operation and schema that would
// leave a generated reference empty.
func openAPIDocumentationProblems(spec *openapi3.T) []string {
	var problems []string
	declaredTags := make(map[string]struct{}, len(spec.Tags))
	for _, tag := range spec.Tags {
		declaredTags[tag.Name] = struct{}{}
	}

	operationIDs := make(map[string]string)
	for _, path := range sortedPathKeys(spec) {
		item := spec.Paths.Value(path)
		if item == nil {
			continue
		}
		for _, method := range sortedOperationMethods(item) {
			op := item.Operations()[method]
			label := fmt.Sprintf("%s %s", method, path)
			if op.OperationID == "" {
				problems = append(problems, label+": missing operationId")
			} else if previous, exists := operationIDs[op.OperationID]; exists {
				problems = append(problems, fmt.Sprintf("%s: operationId %q is already used by %s", label, op.OperationID, previous))
			} else {
				operationIDs[op.OperationID] = label
			}
			if op.Summary == "" {
				problems = append(problems, label+": missing summary")
			}
			if op.Description == "" {
				problems = append(problems, label+": missing description")
			}
			if len(op.Tags) == 0 {
				problems = append(problems, label+": missing tag")
			}
			for _, tag := range op.Tags {
				if _, ok := declaredTags[tag]; !ok {
					problems = append(problems, fmt.Sprintf("%s: tag %q is not declared in the top-level tags", label, tag))
				}
			}
		}
	}

	if spec.Components != nil {
		names := make([]string, 0, len(spec.Components.Schemas))
		for name := range spec.Components.Schemas {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			checkSchemaDocumentation("schema "+name, spec.Components.Schemas[name], &problems)
		}
	}

	return problems
}

// checkSchemaDocumentation requires a description on a schema and on every
// property. Examples are recommended rather than required: a nullable field or
// an object has no single useful example.
func checkSchemaDocumentation(where string, ref *openapi3.SchemaRef, problems *[]string) {
	checkSchema(where, ref, problems, make(map[*openapi3.Schema]bool))
}

// checkSchema walks a schema once per referenced value, so a self-referential
// or diamond-shaped schema cannot make the check loop.
func checkSchema(where string, ref *openapi3.SchemaRef, problems *[]string, seen map[*openapi3.Schema]bool) {
	if ref == nil || ref.Value == nil {
		return
	}
	schema := ref.Value
	if schema.Description == "" {
		*problems = append(*problems, where+": missing description")
	}
	if seen[schema] {
		return
	}
	seen[schema] = true

	names := make([]string, 0, len(schema.Properties))
	for name := range schema.Properties {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		property := schema.Properties[name]
		if property != nil && property.Value != nil && property.Value.Description == "" {
			*problems = append(*problems, where+" property "+name+": missing description")
		}
		checkSchema(where+" property "+name, property, problems, seen)
	}
	if schema.Items != nil {
		checkSchema(where+" items", schema.Items, problems, seen)
	}
	for _, group := range []struct {
		label string
		refs  openapi3.SchemaRefs
	}{
		{"oneOf", schema.OneOf},
		{"anyOf", schema.AnyOf},
		{"allOf", schema.AllOf},
	} {
		for index, member := range group.refs {
			checkSchema(fmt.Sprintf("%s %s[%d]", where, group.label, index), member, problems, seen)
		}
	}
}

func sortedPathKeys(spec *openapi3.T) []string {
	if spec.Paths == nil {
		return nil
	}
	keys := make([]string, 0, spec.Paths.Len())
	for key := range spec.Paths.Map() {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func sortedOperationMethods(item *openapi3.PathItem) []string {
	operations := item.Operations()
	methods := make([]string, 0, len(operations))
	for method := range operations {
		methods = append(methods, method)
	}
	sort.Strings(methods)
	return methods
}

func countOperations(spec *openapi3.T) int {
	total := 0
	for _, path := range sortedPathKeys(spec) {
		if item := spec.Paths.Value(path); item != nil {
			total += len(item.Operations())
		}
	}
	return total
}

func countSchemas(spec *openapi3.T) int {
	if spec.Components == nil {
		return 0
	}
	return len(spec.Components.Schemas)
}

// printOperations prints a compact reference table. It is a convenience for
// humans; the authoritative reference is /api/docs.
func printOperations(stdout io.Writer, spec *openapi3.T) error {
	fmt.Fprintln(stdout, "")
	fmt.Fprintln(stdout, "| Метод | Путь | operationId | Описание |")
	fmt.Fprintln(stdout, "|---|---|---|---|")
	for _, path := range sortedPathKeys(spec) {
		item := spec.Paths.Value(path)
		if item == nil {
			continue
		}
		for _, method := range sortedOperationMethods(item) {
			op := item.Operations()[method]
			fmt.Fprintf(stdout, "| %s | `%s` | `%s` | %s |\n",
				method, path, op.OperationID, op.Summary)
		}
	}
	return nil
}
