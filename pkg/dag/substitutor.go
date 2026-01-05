package dag

// Substitutor defines an interface for substituting variables in a string.
type Substitutor interface {
	// Substitute substitutes variables in the given template string.
	Substitute(template string, scope map[string]string) (string, error)
}
