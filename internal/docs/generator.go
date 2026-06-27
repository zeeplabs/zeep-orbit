package docs

import (
	"fmt"
	"sort"

	"github.com/zeeplabs/zeep-core/internal/registry"
)

// Spec representa a estrutura OpenAPI 3.0.
type Spec struct {
	OpenAPI    string              `json:"openapi"`
	Info       specInfo            `json:"info"`
	Tags       []specTag           `json:"tags,omitempty"`
	Paths      map[string]pathItem `json:"paths"`
	Components components          `json:"components"`
}

type specInfo struct {
	Title   string `json:"title"`
	Version string `json:"version"`
}

type specTag struct {
	Name string `json:"name"`
}

type pathItem struct {
	Get    *operation `json:"get,omitempty"`
	Post   *operation `json:"post,omitempty"`
	Patch  *operation `json:"patch,omitempty"`
	Delete *operation `json:"delete,omitempty"`
}

type operation struct {
	Tags        []string              `json:"tags"`
	Summary     string                `json:"summary"`
	OperationID string                `json:"operationId"`
	Security    []map[string][]string `json:"security"`
	Parameters  []parameter           `json:"parameters,omitempty"`
	RequestBody *requestBody          `json:"requestBody,omitempty"`
	Responses   map[string]response   `json:"responses"`
}

type parameter struct {
	Name     string      `json:"name"`
	In       string      `json:"in"`
	Required bool        `json:"required,omitempty"`
	Schema   schemaOrRef `json:"schema"`
}

type requestBody struct {
	Required bool                 `json:"required"`
	Content  map[string]mediaType `json:"content"`
}

type mediaType struct {
	Schema schemaOrRef `json:"schema"`
}

type response struct {
	Description string               `json:"description"`
	Content     map[string]mediaType `json:"content,omitempty"`
}

type schemaOrRef struct {
	Ref        string                 `json:"$ref,omitempty"`
	Type       string                 `json:"type,omitempty"`
	Format     string                 `json:"format,omitempty"`
	Properties map[string]schemaOrRef `json:"properties,omitempty"`
	Items      *schemaOrRef           `json:"items,omitempty"`
	Required   []string               `json:"required,omitempty"`
	ReadOnly   bool                   `json:"readOnly,omitempty"`
}

type components struct {
	Schemas         map[string]schemaOrRef    `json:"schemas"`
	SecuritySchemes map[string]securityScheme `json:"securitySchemes"`
}

type securityScheme struct {
	Type         string `json:"type"`
	Scheme       string `json:"scheme"`
	BearerFormat string `json:"bearerFormat"`
}

// Generate constrói spec OpenAPI 3.0 com todos os apps do registry.
func Generate(apps []*registry.App) *Spec {
	return generate(apps)
}

// GenerateForApp constrói spec OpenAPI 3.0 filtrada para um único app.
func GenerateForApp(app *registry.App) *Spec {
	return generate([]*registry.App{app})
}

func generate(apps []*registry.App) *Spec {
	spec := &Spec{
		OpenAPI: "3.0.3",
		Info: specInfo{
			Title:   "zeep-core API",
			Version: "1.0.0",
		},
		Paths: make(map[string]pathItem),
		Components: components{
			Schemas: make(map[string]schemaOrRef),
			SecuritySchemes: map[string]securityScheme{
				"bearerAuth": {
					Type:         "http",
					Scheme:       "bearer",
					BearerFormat: "JWT",
				},
			},
		},
	}

	security := []map[string][]string{{"bearerAuth": []string{}}}

	sorted := make([]*registry.App, len(apps))
	copy(sorted, apps)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Config.Name < sorted[j].Config.Name
	})

	for _, app := range sorted {
		appName := app.Config.Name
		spec.Tags = append(spec.Tags, specTag{Name: appName})

		tableNames := make([]string, 0, len(app.Tables))
		for name := range app.Tables {
			tableNames = append(tableNames, name)
		}
		sort.Strings(tableNames)

		for _, tableName := range tableNames {
			table := app.Tables[tableName]

			schemaName := fmt.Sprintf("%s_%s", appName, tableName)
			inputName := schemaName + "_input"
			listName := schemaName + "_list"

			spec.Components.Schemas[schemaName] = buildResponseSchema(table)
			spec.Components.Schemas[inputName] = buildInputSchema(table)
			spec.Components.Schemas[listName] = schemaOrRef{
				Type: "object",
				Properties: map[string]schemaOrRef{
					"data":   {Type: "array", Items: &schemaOrRef{Ref: "#/components/schemas/" + schemaName}},
					"count":  {Type: "integer"},
					"limit":  {Type: "integer"},
					"offset": {Type: "integer"},
				},
			}

			collectionPath := fmt.Sprintf("/%s/%s/", appName, tableName)
			itemPath := fmt.Sprintf("/%s/%s/{id}/", appName, tableName)

			idParam := parameter{
				Name:     "id",
				In:       "path",
				Required: true,
				Schema:   schemaOrRef{Type: "string", Format: "uuid"},
			}

			spec.Paths[collectionPath] = pathItem{
				Get: &operation{
					Tags:        []string{appName},
					Summary:     fmt.Sprintf("List %s", tableName),
					OperationID: fmt.Sprintf("list_%s_%s", appName, tableName),
					Security:    security,
					Parameters: []parameter{
						{Name: "limit", In: "query", Schema: schemaOrRef{Type: "integer", Format: "int32"}},
						{Name: "offset", In: "query", Schema: schemaOrRef{Type: "integer", Format: "int32"}},
						{Name: "order", In: "query", Schema: schemaOrRef{Type: "string"}},
					},
					Responses: map[string]response{
						"200": {Description: "OK", Content: jsonContent(schemaOrRef{Ref: "#/components/schemas/" + listName})},
						"401": {Description: "Unauthorized"},
					},
				},
				Post: &operation{
					Tags:        []string{appName},
					Summary:     fmt.Sprintf("Create %s", tableName),
					OperationID: fmt.Sprintf("create_%s_%s", appName, tableName),
					Security:    security,
					RequestBody: &requestBody{
						Required: true,
						Content:  jsonContent(schemaOrRef{Ref: "#/components/schemas/" + inputName}),
					},
					Responses: map[string]response{
						"201": {Description: "Created", Content: jsonContent(schemaOrRef{Ref: "#/components/schemas/" + schemaName})},
						"400": {Description: "Bad Request"},
						"401": {Description: "Unauthorized"},
					},
				},
			}

			spec.Paths[itemPath] = pathItem{
				Get: &operation{
					Tags:        []string{appName},
					Summary:     fmt.Sprintf("Get %s by ID", tableName),
					OperationID: fmt.Sprintf("get_%s_%s_by_id", appName, tableName),
					Security:    security,
					Parameters:  []parameter{idParam},
					Responses: map[string]response{
						"200": {Description: "OK", Content: jsonContent(schemaOrRef{Ref: "#/components/schemas/" + schemaName})},
						"401": {Description: "Unauthorized"},
						"404": {Description: "Not Found"},
					},
				},
				Patch: &operation{
					Tags:        []string{appName},
					Summary:     fmt.Sprintf("Update %s", tableName),
					OperationID: fmt.Sprintf("update_%s_%s", appName, tableName),
					Security:    security,
					Parameters:  []parameter{idParam},
					RequestBody: &requestBody{
						Required: true,
						Content:  jsonContent(schemaOrRef{Ref: "#/components/schemas/" + inputName}),
					},
					Responses: map[string]response{
						"200": {Description: "OK", Content: jsonContent(schemaOrRef{Ref: "#/components/schemas/" + schemaName})},
						"400": {Description: "Bad Request"},
						"401": {Description: "Unauthorized"},
						"404": {Description: "Not Found"},
					},
				},
				Delete: &operation{
					Tags:        []string{appName},
					Summary:     fmt.Sprintf("Delete %s", tableName),
					OperationID: fmt.Sprintf("delete_%s_%s", appName, tableName),
					Security:    security,
					Parameters:  []parameter{idParam},
					Responses: map[string]response{
						"204": {Description: "No Content"},
						"401": {Description: "Unauthorized"},
						"404": {Description: "Not Found"},
					},
				},
			}
		}
	}

	return spec
}

func buildResponseSchema(table *registry.Table) schemaOrRef {
	props := map[string]schemaOrRef{
		"id":         {Type: "string", Format: "uuid", ReadOnly: true},
		"created_at": {Type: "string", Format: "date-time", ReadOnly: true},
		"updated_at": {Type: "string", Format: "date-time", ReadOnly: true},
	}
	required := []string{"id", "created_at", "updated_at"}

	for _, col := range table.Columns {
		t, f := openAPIType(col.Type)
		props[col.Name] = schemaOrRef{Type: t, Format: f}
		if col.Required {
			required = append(required, col.Name)
		}
	}

	return schemaOrRef{Type: "object", Properties: props, Required: required}
}

func buildInputSchema(table *registry.Table) schemaOrRef {
	props := make(map[string]schemaOrRef, len(table.Columns))
	var required []string

	for _, col := range table.Columns {
		t, f := openAPIType(col.Type)
		props[col.Name] = schemaOrRef{Type: t, Format: f}
		if col.Required {
			required = append(required, col.Name)
		}
	}

	return schemaOrRef{Type: "object", Properties: props, Required: required}
}

func openAPIType(zeepType string) (typ, format string) {
	switch zeepType {
	case "integer":
		return "integer", "int32"
	case "bigint":
		return "integer", "int64"
	case "decimal":
		return "number", "double"
	case "boolean":
		return "boolean", ""
	case "uuid":
		return "string", "uuid"
	case "timestamptz":
		return "string", "date-time"
	case "jsonb":
		return "object", ""
	default:
		return "string", ""
	}
}

func jsonContent(s schemaOrRef) map[string]mediaType {
	return map[string]mediaType{"application/json": {Schema: s}}
}
