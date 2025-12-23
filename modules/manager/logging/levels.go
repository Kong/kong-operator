package logging

// Level represents the log level.
type Level int

const (
	// InfoLevel is the default log level.
	InfoLevel Level = 0
	// DebugLevel is the debug log level.
	DebugLevel Level = 1
	// TraceLevel is the trace log level.
	TraceLevel Level = 2
)

// String returns the string representation of the log level.
func (l Level) String() string {
	switch l {
	case InfoLevel:
		return "info"
	case DebugLevel:
		return "debug"
	case TraceLevel:
		return "trace"
	}

	return ""
}

// Value returns the integer value of the log level.
func (l Level) Value() int {
	return (int)(l)
}
