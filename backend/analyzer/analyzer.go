package analyzer

type Graph struct {
	Entrypoints []string           `json:"entrypoints"`
	Functions   []Function         `json:"functions"`
	Structs     []StructDefinition `json:"structs"`
	Creations   []Creation         `json:"creations"`
	Edges       []Edge             `json:"edges"`
}

type Function struct {
	ID          string `json:"id"`
	Description string `json:"description"`
	File        string `json:"file"`
	Code        string `json:"code"`
}

type StructDefinition struct {
	ID          string `json:"id"`
	Description string `json:"description"`
	File        string `json:"file"`
	Code        string `json:"code"`
}

type Creation struct {
	ID         string `json:"id"`
	Type       string `json:"type"`
	Definition string `json:"definition,omitempty"`
}

type Edge struct {
	Caller string `json:"caller"`
	Target string `json:"target"`
	Line   int    `json:"line"`
}
