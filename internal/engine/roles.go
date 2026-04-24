package engine

import "fmt"

// PickPair decides which engine codes and which reviews for a task of the
// given kind. If rolesByKind has an entry for kind, that engine codes and the
// other registered engine reviews. Otherwise coderDefault codes and
// reviewerDefault reviews.
//
// Cross-model review is mandatory: PickPair returns an error if the resolved
// coder and reviewer would be the same engine. Config.Load enforces this at
// load time, but the guard is duplicated here so that programmatic callers
// (tests, future APIs) cannot accidentally pair an engine with itself.
func PickPair(kind string, rolesByKind map[string]string, coderDefault, reviewerDefault string, engines map[string]Engine) (Engine, Engine, error) {
	if coderDefault == reviewerDefault {
		return nil, nil, fmt.Errorf(
			"cross-model review requires coderDefault != reviewerDefault (both %q)",
			coderDefault)
	}
	coderName := coderDefault
	if v, ok := rolesByKind[kind]; ok && v != "" {
		coderName = v
	}
	coder, ok := engines[coderName]
	if !ok {
		return nil, nil, fmt.Errorf("engine %q not registered", coderName)
	}
	// Reviewer is the "other" — if only two engines, pick the one that isn't coder.
	var reviewerName string
	if coderName == coderDefault {
		reviewerName = reviewerDefault
	} else {
		reviewerName = coderDefault
	}
	if reviewerName == coderName {
		return nil, nil, fmt.Errorf(
			"cross-model review violated: coder and reviewer resolved to %q", coderName)
	}
	reviewer, ok := engines[reviewerName]
	if !ok {
		return nil, nil, fmt.Errorf("engine %q not registered", reviewerName)
	}
	return coder, reviewer, nil
}
