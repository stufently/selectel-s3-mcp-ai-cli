// Package safety is the single authority for gates, scope, filesystem confinement and redaction.
package safety

import (
	"context"
	"fmt"
)

type Elicitor func(context.Context, string) (bool, error)
type Gate struct{ Policy string }

func (g Gate) Check(ctx context.Context, confirm bool, ask Elicitor, effect string) error {
	switch g.Policy {
	case "allow":
		return nil
	case "block":
		return fmt.Errorf("blocked: %s", effect)
	case "confirm", "elicit":
		if ask != nil {
			accepted, e := ask(ctx, effect)
			if e != nil {
				return fmt.Errorf("confirmation failed: %w", e)
			}
			if accepted {
				return nil
			}
			return fmt.Errorf("declined: %s", effect)
		}
		if g.Policy == "elicit" {
			return fmt.Errorf("elicitation_required: client must support form elicitation: %s", effect)
		}
		if confirm {
			return nil
		}
		return fmt.Errorf("confirmation_required: human approval needed: %s; use confirm:true after approval", effect)
	default:
		return fmt.Errorf("invalid gate policy")
	}
}
