package log

import "gerrit-share.lan/go/inspect"

type Severity int

func (s *Severity) Embed(i *inspect.ObjectInspector) {
	str := SeverityStrings[*s]
	i.String(&str, "severity", true, "severity level")
	if i.IsReading() {
		key, ok := severityByString[str]
		if ok {
			*s = key
		}
	}
}

func (s Severity) MarshalText() ([]byte, error) {
	if s == SeverityUnsupported {
		return nil, ErrUnsupportedSeverity
	}
	result, ok := SeverityStrings[s]
	if !ok {
		return nil, ErrUnsupportedSeverity
	}
	return []byte(result), nil
}

func (s *Severity) UnmarshalText(data []byte) error {
	var ok bool
	*s, ok = severityByString[string(data)]
	if !ok {
		*s = SeverityUnsupported
	}
	return nil
}

const (
	SeverityUnsupported Severity = iota
	SeverityCrash
	SeverityCritical
	SeverityError
	SeverityWarning
	SeverityProcessing
	SeverityStatus
	SeverityInfo
	SeverityDebug
)

var SeverityStrings = map[Severity]string{
	SeverityCrash:      "crash",
	SeverityCritical:   "critical",
	SeverityError:      "error",
	SeverityWarning:    "warning",
	SeverityProcessing: "processing",
	SeverityStatus:     "status",
	SeverityInfo:       "info",
	SeverityDebug:      "debug"}

var severityByString = func() map[string]Severity {
	result := make(map[string]Severity, len(SeverityStrings))
	for key, value := range SeverityStrings {
		result[value] = key
	}
	return result
}()

type Verbosity int

const (
	VerbosityUndefined Verbosity = iota
	VerbosityLowest
	VerbosityLow
	VerbosityNormal
	VerbosityHigh
	VerbosityHighest
)

var severityByVerbosity = map[Verbosity]Severity{
	VerbosityUndefined: SeverityInfo,
	VerbosityLowest:    SeverityError,
	VerbosityLow:       SeverityWarning,
	VerbosityNormal:    SeverityInfo,
	VerbosityHigh:      SeverityInfo,
	VerbosityHighest:   SeverityDebug}

func GetSeverity() Severity {
	severityLevel, ok := severityByVerbosity[GetVerbosity()]
	if !ok {
		return SeverityInfo
	}
	return severityLevel
}