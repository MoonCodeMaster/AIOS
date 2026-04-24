package engine

import "fmt"

// PickPair decides which engine codes and which reviews for a task of the
// given kind. If rolesByKind has an entry for kind, that engine codes and the
// other registered engine reviews. Otherwise coderDefault codes and
// reviewerDefault reviews.
func PickPair(kind string, rolesByKind map[string]string, coderDefault, reviewerDefault string, engines map[string]Engine) (Engine, Engine, error) {
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
	reviewer, ok := engines[reviewerName]
	if !ok {
		return nil, nil, fmt.Errorf("engine %q not registered", reviewerName)
	}
	return coder, reviewer, nil
}
