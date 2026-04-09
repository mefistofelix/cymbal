package symbols

// Symbol represents a parsed code symbol.
type Symbol struct {
	Name      string `json:"name"`
	Kind      string `json:"kind"`
	File      string `json:"file"`
	StartLine int    `json:"start_line"`
	EndLine   int    `json:"end_line"`
	StartCol  int    `json:"start_col,omitempty"`
	EndCol    int    `json:"end_col,omitempty"`
	Parent    string `json:"parent,omitempty"`
	Depth     int    `json:"depth"`
	Signature string `json:"signature,omitempty"`
	Language  string `json:"language"`
}

// Import represents an import/use statement found in source.
type Import struct {
	RawPath  string `json:"raw_path"`
	Language string `json:"language"`
}

// Ref represents a reference to an identifier (call expression, usage).
type Ref struct {
	Name     string `json:"name"`
	Line     int    `json:"line"`
	Language string `json:"language"`
}

// ParseResult holds all extracted data from a file.
type ParseResult struct {
	Symbols []Symbol
	Imports []Import
	Refs    []Ref
}
