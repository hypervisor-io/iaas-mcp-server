package tools

// Shared result types for the coverage-parity tools.

// ItemsResult is the generic list output for parity read tools.
type ItemsResult struct {
	Items []map[string]any `json:"items"`
	Count int              `json:"count"`
}

func itemsResult(items []map[string]any, err error) (ItemsResult, error) {
	if err != nil {
		return ItemsResult{}, err
	}
	return ItemsResult{Items: items, Count: len(items)}, nil
}

// ObjectResult is the generic single-object output for parity action tools that
// return one object (an interface, forge state, health check, queue handle...).
type ObjectResult struct {
	Object map[string]any `json:"object"`
}

func objectResult(obj map[string]any, err error) (ObjectResult, error) {
	if err != nil {
		return ObjectResult{}, err
	}
	return ObjectResult{Object: obj}, nil
}
