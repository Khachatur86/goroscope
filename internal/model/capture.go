package model

type Capture struct {
	Name      string          `json:"name"`
	Target    string          `json:"target,omitempty"`
	Events    []Event         `json:"events"`
	Stacks    []StackSnapshot `json:"stacks,omitempty"`
	Resources []ResourceEdge  `json:"resources,omitempty"`
}
