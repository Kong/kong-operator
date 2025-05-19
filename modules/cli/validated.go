package cli

import "fmt"

// validatedValueOpt is a function that modifies a ValidatedValue.
type validatedValueOpt[T any] func(*validatedValue[T])

// withDefault sets the default value for the validated variable.
func withDefault[T any](defaultValue T) validatedValueOpt[T] {
	return func(v *validatedValue[T]) {
		*v.variable = defaultValue

		// Assign origin which is used in ValidatedValue[T]'s String() string
		// func so that we get a pretty printed default.
		v.origin = stringFromAny(defaultValue)
	}
}

func stringFromAny(s any) string {
	switch ss := s.(type) {
	case string:
		return ss
	case fmt.Stringer:
		return fmt.Sprintf("%q", ss.String())
	default:
		panic(fmt.Errorf("unknown type %T", s))
	}
}

// validatedValue implements `pflag.Value` interface. It can be used for hooking up arbitrary validation logic to any type.
// It should be passed to `pflag.FlagSet.Var()`.
type validatedValue[T any] struct {
	origin      string
	variable    *T
	constructor func(string) (T, error)
	typeName    string
}

// newValidatedValue creates a validated variable of type T. Constructor should validate the input and return an error
// in case of any failures. If validation passes, constructor should return a value that's to be set in the variable.
// The constructor accepts a flagValue that is raw input from user's command line (or an env variable that was bind to
// the flag, see: bindEnvVars).
// It accepts a variadic list of options that can be used e.g. to set the default value or override the type name.
func newValidatedValue[T any](variable *T, constructor func(flagValue string) (T, error), opts ...validatedValueOpt[T]) validatedValue[T] {
	v := validatedValue[T]{
		constructor: constructor,
		variable:    variable,
	}
	for _, opt := range opts {
		opt(&v)
	}
	return v
}

func (v validatedValue[T]) String() string {
	return v.origin
}

// Set sets the value of the variable. It uses the constructor to validate the input and set the value.
func (v validatedValue[T]) Set(s string) error {
	value, err := v.constructor(s)
	if err != nil {
		return err
	}

	*v.variable = value
	return nil
}

// Type returns the type of the variable. If the type name is overridden, it returns that.
func (v validatedValue[T]) Type() string {
	if v.typeName != "" {
		return v.typeName
	}

	var t T
	return fmt.Sprintf("%T", t)
}
