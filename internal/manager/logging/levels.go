package logging

type Level int

const (
	InfoLevel  Level = 0
	DebugLevel Level = 1
	TraceLevel Level = 2
)

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

func (l Level) Value() int {
	return (int)(l)
}
