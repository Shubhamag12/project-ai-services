package bootstrap

// Rule defines the interface for validation rules
type Rule interface {
	Verify() error
	Hint() string
	String() string
}
