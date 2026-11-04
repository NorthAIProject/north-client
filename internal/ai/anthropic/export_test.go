package anthropic

import "time"

// ClientFirstTokenBudget exposes the budget a client was built with, so a test
// can check the production default without waiting it out.
func ClientFirstTokenBudget(c *Client) time.Duration { return c.firstTokenBudget }
