package costguard

import (
	"errors"
	"fmt"
)

type CostguardError struct {
	StatusCode int
	Category   string
	Message    string
	Type       string
}

func (e *CostguardError) Error() string {
	return fmt.Sprintf("costguard %d (%s): %s", e.StatusCode, e.Category, e.Message)
}

func IsBudgetExceeded(err error) bool {
	var e *CostguardError
	return errors.As(err, &e) && e.StatusCode == 402
}

func IsRateLimited(err error) bool {
	var e *CostguardError
	return errors.As(err, &e) && e.StatusCode == 429
}

func IsProviderDown(err error) bool {
	var e *CostguardError
	return errors.As(err, &e) && (e.StatusCode == 502 || e.StatusCode == 503)
}
